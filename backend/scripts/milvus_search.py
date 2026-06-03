#!/usr/bin/env python3
"""
Search tool for the RAG Milvus vector store.

Reads config from configs/local.yaml (same file the Go backend uses).
Collection name = knowledge_base_id.

Schema (defined in internal/rag/milvus.go):
  chunk_id         VARCHAR PK
  knowledge_base_id  VARCHAR
  document_id      VARCHAR
  filename         VARCHAR
  section_title    VARCHAR
  page_start       INT32
  page_end         INT32
  chunk_index      INT32
  file_sha256      VARCHAR
  tags             ARRAY<VARCHAR>
  text             VARCHAR  (BM25-indexed)
  embedding        FLOAT_VECTOR
  sparse           SPARSE_FLOAT_VECTOR  (auto-generated from text via BM25 function)

Install:
    pip install pymilvus openai orjson pyyaml

Example config (configs/local.yaml):

  embedder:
    base_url: "https://..."
    api_key:  "sk-..."
    model:    "text-embedding-v4"
    dim:      1024
    batch_size: 10

  milvus:
    addr: "milvus.localhost:80"
    db:   "rag"
    api_key: ""         # optional

Usage:
    python3 milvus_search.py --kb <knowledge_base_id> stats
    python3 milvus_search.py --kb <knowledge_base_id> dense --query "文档处理流程" --top-k 5
    python3 milvus_search.py --kb <knowledge_base_id> bm25  --query "文档处理" --top-k 5
    python3 milvus_search.py --kb <knowledge_base_id> dense  --query "..." --doc-ids id1,id2
    python3 milvus_search.py --kb <knowledge_base_id> bm25   --query "..." --tags 合规,审批
    python3 milvus_search.py --kb <knowledge_base_id> hybrid --query "..." --top-k 5 --rrfk 60
"""

from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from typing import Any

import orjson
import yaml
from openai import OpenAI
from pymilvus import (
    AnnSearchRequest,
    MilvusClient,
    MilvusException,
    RRFRanker,
)

DEFAULT_CONFIG_PATH = "configs/local.yaml"

# Mirrors searchOutputFields in internal/rag/milvus.go
SEARCH_OUTPUT_FIELDS = [
    "document_id",
    "filename",
    "file_sha256",
    "knowledge_base_id",
    "chunk_id",
    "chunk_index",
    "section_title",
    "page_start",
    "page_end",
    "tags",
    "text",
]


@dataclass
class Settings:
    milvus_uri: str
    milvus_token: str
    milvus_db_name: str
    embedding_base_url: str
    embedding_api_key: str
    embedding_model: str
    embedding_dim: int
    embedding_batch_size: int

def _jsonable(v: Any) -> Any:
    """Convert pymilvus/protobuf/numpy-like values into plain JSON types."""
    if isinstance(v, (str, int, float, bool, type(None))):
        return v
    if isinstance(v, bytes):
        return v.decode("utf-8", errors="replace")
    if hasattr(v, "item") and callable(v.item):  # numpy scalar etc.
        try:
            return _jsonable(v.item())
        except Exception:
            pass
    if isinstance(v, dict) or hasattr(v, "items"):
        try:
            return {str(k): _jsonable(val) for k, val in v.items()}
        except Exception:
            pass
    if hasattr(v, "__iter__"):
        try:
            return [_jsonable(x) for x in v]
        except Exception:
            pass
    return str(v)


def jprint(obj: Any) -> None:
    sys.stdout.buffer.write(orjson.dumps(_jsonable(obj), option=orjson.OPT_INDENT_2))
    sys.stdout.buffer.write(b"\n")


def _to_uri(addr: str) -> str:
    """Normalize bare host:port to http://host:port for pymilvus."""
    if "://" in addr:
        return addr
    return f"http://{addr}"


def read_yaml_config(path: str) -> Settings:
    from pathlib import Path

    cfg_path = Path(path)
    if not cfg_path.exists():
        raise FileNotFoundError(f"Config file not found: {cfg_path}")

    with cfg_path.open("r", encoding="utf-8") as f:
        raw = yaml.safe_load(f) or {}

    milvus = raw.get("milvus", {})
    embedder = raw.get("embedder", {})

    return Settings(
        milvus_uri=_to_uri(milvus.get("addr", "127.0.0.1:19530")),
        milvus_token=str(milvus.get("api_key", "") or ""),
        milvus_db_name=str(milvus.get("db", "default") or "default"),
        embedding_base_url=embedder.get("base_url", "http://127.0.0.1:8000/v1"),
        embedding_api_key=str(embedder.get("api_key", "dummy") or "dummy"),
        embedding_model=str(embedder.get("model", "text-embedding-v4")),
        embedding_dim=int(embedder.get("dim", 1024)),
        embedding_batch_size=int(embedder.get("batch_size", 10)),
    )


class EmbeddingClient:
    def __init__(self, settings: Settings):
        self.settings = settings
        self.client = OpenAI(
            api_key=settings.embedding_api_key,
            base_url=settings.embedding_base_url,
        )

    def embed_query(self, text: str) -> list[float]:
        response = self.client.embeddings.create(
            model=self.settings.embedding_model,
            input=[text],
        )
        vector = response.data[0].embedding
        if len(vector) != self.settings.embedding_dim:
            raise ValueError(
                f"Embedding dimension mismatch: expected {self.settings.embedding_dim}, got {len(vector)}"
            )
        return vector


class MilvusStore:
    def __init__(self, settings: Settings):
        self.settings = settings
        kwargs: dict[str, Any] = {
            "uri": self.settings.milvus_uri,
            "db_name": self.settings.milvus_db_name,
        }
        if self.settings.milvus_token:
            kwargs["token"] = self.settings.milvus_token
        self.client = MilvusClient(**kwargs)

    def _ensure_loaded(self, kb_id: str) -> None:
        if not self.client.has_collection(kb_id):
            raise MilvusException(code=800, message=f"knowledge base (collection) not found: {kb_id!r}")
        self.client.load_collection(kb_id)

    @staticmethod
    def _build_filter(
        doc_ids: list[str] | None,
        tags: list[str] | None,
        filenames: list[str] | None,
    ) -> str | None:
        def _esc(s: str) -> str:
            return s.replace("\\", "\\\\").replace('"', '\\"')

        def _varchar_in(field: str, values: list[str]) -> str:
            if len(values) == 1:
                return f'{field} == "{_esc(values[0])}"'
            joined = ", ".join(f'"{_esc(v)}"' for v in values)
            return f"{field} in [{joined}]"

        parts: list[str] = []
        if doc_ids:
            parts.append(_varchar_in("document_id", doc_ids))
        if filenames:
            parts.append(_varchar_in("filename", filenames))
        if tags:
            # OR logic: match chunks that carry any of the requested tags
            clauses = " or ".join(f'ARRAY_CONTAINS(tags, "{_esc(t)}")' for t in tags)
            parts.append(f"({clauses})" if len(tags) > 1 else clauses)
        return " and ".join(parts) if parts else None

    @staticmethod
    def _hits_from_search(raw: Any) -> list[dict[str, Any]]:
        hits = []
        for hit in raw[0]:
            item: dict[str, Any] = {"score": float(hit["distance"])}
            item.update(hit.get("entity", {}))
            hits.append(item)
        return hits

    def search(
        self,
        kb_id: str,
        vector: list[float],
        top_k: int = 5,
        doc_ids: list[str] | None = None,
        tags: list[str] | None = None,
        filenames: list[str] | None = None,
        ef: int | None = None,
    ) -> list[dict[str, Any]]:
        self._ensure_loaded(kb_id)
        expr = self._build_filter(doc_ids, tags, filenames)
        search_params: dict[str, Any] = {"metric_type": "COSINE", "params": {}}
        if ef and ef > 0:
            search_params["params"]["ef"] = ef

        raw = self.client.search(
            collection_name=kb_id,
            data=[vector],
            anns_field="embedding",
            search_params=search_params,
            limit=top_k,
            filter=expr or "",
            output_fields=SEARCH_OUTPUT_FIELDS,
        )
        return self._hits_from_search(raw)

    def full_text_search(
        self,
        kb_id: str,
        query: str,
        top_k: int = 5,
        doc_ids: list[str] | None = None,
        tags: list[str] | None = None,
        filenames: list[str] | None = None,
        drop_ratio: float | None = None,
    ) -> list[dict[str, Any]]:
        self._ensure_loaded(kb_id)
        expr = self._build_filter(doc_ids, tags, filenames)
        search_params: dict[str, Any] = {"metric_type": "BM25", "params": {}}
        if drop_ratio and drop_ratio > 0:
            search_params["params"]["drop_ratio_search"] = drop_ratio

        raw = self.client.search(
            collection_name=kb_id,
            data=[query],
            anns_field="sparse",
            search_params=search_params,
            limit=top_k,
            filter=expr or "",
            output_fields=SEARCH_OUTPUT_FIELDS,
        )
        return self._hits_from_search(raw)

    def hybrid_search(
        self,
        kb_id: str,
        vector: list[float],
        query: str,
        top_k: int = 5,
        doc_ids: list[str] | None = None,
        tags: list[str] | None = None,
        filenames: list[str] | None = None,
        ef: int | None = None,
        drop_ratio: float | None = None,
        rrfk: int = 60,
    ) -> list[dict[str, Any]]:
        self._ensure_loaded(kb_id)
        expr = self._build_filter(doc_ids, tags, filenames)

        dense_params: dict[str, Any] = {"metric_type": "COSINE", "params": {}}
        if ef and ef > 0:
            dense_params["params"]["ef"] = ef
        dense_req = AnnSearchRequest(
            data=[vector],
            anns_field="embedding",
            param=dense_params,
            limit=top_k,
            expr=expr,
        )

        bm25_params: dict[str, Any] = {"metric_type": "BM25", "params": {}}
        if drop_ratio and drop_ratio > 0:
            bm25_params["params"]["drop_ratio_search"] = drop_ratio
        bm25_req = AnnSearchRequest(
            data=[query],
            anns_field="sparse",
            param=bm25_params,
            limit=top_k,
            expr=expr,
        )

        raw = self.client.hybrid_search(
            collection_name=kb_id,
            reqs=[dense_req, bm25_req],
            ranker=RRFRanker(k=rrfk),
            limit=top_k,
            output_fields=SEARCH_OUTPUT_FIELDS,
        )
        return self._hits_from_search(raw)

    def stats(self, kb_id: str) -> dict[str, Any]:
        if not self.client.has_collection(kb_id):
            return {"exists": False, "knowledge_base_id": kb_id}
        self.client.load_collection(kb_id)
        collection_stats = self.client.get_collection_stats(kb_id)
        index_names = self.client.list_indexes(kb_id)
        indexes = []
        for idx_name in index_names:
            idx_info = self.client.describe_index(kb_id, idx_name)
            indexes.append({
                "field": idx_info.get("field_name", idx_name),
                "index_type": idx_info.get("index_type", ""),
                "metric_type": idx_info.get("metric_type", ""),
            })
        return {
            "exists": True,
            "knowledge_base_id": kb_id,
            "num_entities": int(collection_stats.get("row_count", 0)),
            "indexes": indexes,
        }


def _to_openclaw(hits: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "results": [
            {
                "id": h.get("chunk_id"),
                "score": h.get("score"),
                "text": h.get("text"),
                "metadata": {
                    "document_id": h.get("document_id"),
                    "knowledge_base_id": h.get("knowledge_base_id"),
                    "filename": h.get("filename"),
                    "file_sha256": h.get("file_sha256"),
                    "section_title": h.get("section_title"),
                    "page_start": h.get("page_start"),
                    "page_end": h.get("page_end"),
                    "chunk_index": h.get("chunk_index"),
                    "tags": h.get("tags"),
                },
            }
            for h in hits
        ]
    }


def _parse_csv(value: str | None) -> list[str] | None:
    if not value:
        return None
    return [v.strip() for v in value.split(",") if v.strip()] or None


def cmd_stats(args: argparse.Namespace, settings: Settings) -> None:
    store = MilvusStore(settings)
    jprint(store.stats(args.kb))


def cmd_dense(args: argparse.Namespace, settings: Settings) -> None:
    embedder = EmbeddingClient(settings)
    vector = embedder.embed_query(args.query)
    store = MilvusStore(settings)
    hits = store.search(
        kb_id=args.kb,
        vector=vector,
        top_k=args.top_k,
        doc_ids=_parse_csv(args.doc_ids),
        tags=_parse_csv(args.tags),
        filenames=_parse_csv(args.filenames),
        ef=args.ef,
    )
    if args.openclaw_format:
        jprint(_to_openclaw(hits))
    else:
        jprint({
            "ok": True,
            "action": "dense",
            "knowledge_base_id": args.kb,
            "query": args.query,
            "top_k": args.top_k,
            "hits": hits,
        })


def cmd_bm25(args: argparse.Namespace, settings: Settings) -> None:
    store = MilvusStore(settings)
    hits = store.full_text_search(
        kb_id=args.kb,
        query=args.query,
        top_k=args.top_k,
        doc_ids=_parse_csv(args.doc_ids),
        tags=_parse_csv(args.tags),
        filenames=_parse_csv(args.filenames),
        drop_ratio=args.drop_ratio,
    )
    if args.openclaw_format:
        jprint(_to_openclaw(hits))
    else:
        jprint({
            "ok": True,
            "action": "bm25",
            "knowledge_base_id": args.kb,
            "query": args.query,
            "top_k": args.top_k,
            "hits": hits,
        })


def cmd_hybrid(args: argparse.Namespace, settings: Settings) -> None:
    embedder = EmbeddingClient(settings)
    vector = embedder.embed_query(args.query)
    store = MilvusStore(settings)
    hits = store.hybrid_search(
        kb_id=args.kb,
        vector=vector,
        query=args.query,
        top_k=args.top_k,
        doc_ids=_parse_csv(args.doc_ids),
        tags=_parse_csv(args.tags),
        filenames=_parse_csv(args.filenames),
        ef=args.ef,
        drop_ratio=args.drop_ratio,
        rrfk=args.rrfk,
    )
    if args.openclaw_format:
        jprint(_to_openclaw(hits))
    else:
        jprint({
            "ok": True,
            "action": "hybrid",
            "knowledge_base_id": args.kb,
            "query": args.query,
            "top_k": args.top_k,
            "hits": hits,
        })


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Search the RAG Milvus vector store")
    parser.add_argument("--config", default=DEFAULT_CONFIG_PATH, help="Path to YAML config")
    parser.add_argument("--kb", required=True, help="Knowledge base ID (= Milvus collection name)")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("stats", help="Show collection stats")

    p = sub.add_parser("dense", help="Dense vector (semantic) search")
    p.add_argument("--query", required=True, help="Query text to embed and search")
    p.add_argument("--top-k", type=int, default=5, help="Number of results to return")
    p.add_argument("--doc-ids", default=None, help="Comma-separated document IDs to restrict search to")
    p.add_argument("--filenames", default=None, help="Comma-separated filenames to restrict search to")
    p.add_argument("--tags", default=None, help="Comma-separated tags; OR logic (any match passes)")
    p.add_argument("--ef", type=int, default=None, help="HNSW ef parameter (must be >= top-k)")
    p.add_argument("--openclaw-format", action="store_true", help="Output in OpenClaw results format")

    p = sub.add_parser("bm25", help="BM25 full-text keyword search")
    p.add_argument("--query", required=True, help="Query text for BM25 search")
    p.add_argument("--top-k", type=int, default=5, help="Number of results to return")
    p.add_argument("--doc-ids", default=None, help="Comma-separated document IDs to restrict search to")
    p.add_argument("--filenames", default=None, help="Comma-separated filenames to restrict search to")
    p.add_argument("--tags", default=None, help="Comma-separated tags; OR logic (any match passes)")
    p.add_argument("--drop-ratio", type=float, default=None,
                   help="Fraction of low-frequency BM25 terms to drop (0.0-1.0)")
    p.add_argument("--openclaw-format", action="store_true", help="Output in OpenClaw results format")

    p = sub.add_parser("hybrid", help="Hybrid search: dense + BM25 with RRF reranking")
    p.add_argument("--query", required=True, help="Query text (used for both embedding and BM25)")
    p.add_argument("--top-k", type=int, default=5, help="Number of results to return")
    p.add_argument("--doc-ids", default=None, help="Comma-separated document IDs to restrict search to")
    p.add_argument("--filenames", default=None, help="Comma-separated filenames to restrict search to")
    p.add_argument("--tags", default=None, help="Comma-separated tags; OR logic (any match passes)")
    p.add_argument("--ef", type=int, default=None, help="HNSW ef parameter for dense leg (must be >= top-k)")
    p.add_argument("--drop-ratio", type=float, default=None,
                   help="Fraction of low-frequency BM25 terms to drop (0.0-1.0)")
    p.add_argument("--rrfk", type=int, default=60, help="RRF k parameter for score fusion (default: 60)")
    p.add_argument("--openclaw-format", action="store_true", help="Output in OpenClaw results format")

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()

    try:
        settings = read_yaml_config(args.config)
        if args.command == "stats":
            cmd_stats(args, settings)
        elif args.command == "dense":
            cmd_dense(args, settings)
        elif args.command == "bm25":
            cmd_bm25(args, settings)
        elif args.command == "hybrid":
            cmd_hybrid(args, settings)
        else:
            parser.error(f"unknown command: {args.command}")
    except MilvusException as exc:
        jprint({"ok": False, "error": "milvus_error", "detail": str(exc)})
        return 2
    except Exception as exc:  # noqa: BLE001
        jprint({"ok": False, "error": exc.__class__.__name__, "detail": str(exc)})
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
