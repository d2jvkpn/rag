# RAG 文档处理流程与业务规则

相关文档：

- [设计决策与关键约定](./design.md)
- [系统架构总览](./architecture.md)
- [API 设计](./api.md)

## 目标

先支持四类输入文档：

- `pdf`
- `docx`
- `pptx`
- `markdown`

## 业务流程

完整链路建议为：

1. 上传文档
2. 解析文档
3. 清洗文本
4. 自动切分 chunks
5. 可选：用户人工审核 chunks
6. 生成 embedding
7. 写入 Milvus
8. 提供删除、重建、状态查询

第一版默认不强制人工审核。系统可在自动切分后直接进入 embedding 和入库流程；如果业务开启审核，则在入库前进入审核阶段。

## 上传阶段

输入：

- 文件二进制
- `knowledge_base_id`（必填，文档归属边界，同时作为存储路径命名空间和检索过滤边界，详见 [设计决策与关键约定](./design.md#知识库与-knowledge_base_id)）
- 可选：`doc_id`、`title`、`tags`

落盘建议：

- 原文件存后端本地文件目录
- 生成唯一 `document_id`
- 记录 `sha256`，用于去重

初始化状态：

- `uploaded`

## 解析阶段

### PDF

优先路径：

- 可提取文本 PDF：通过 Python 生态工具直接抽文本

后续可扩展：

- 扫描版 PDF：走 OCR

当前阶段可以先不做 OCR，遇到纯图片 PDF 直接标记失败，错误原因写清楚。

### DOCX

提取内容：

- 段落
- 标题层级
- 表格文本

建议先把图片、页眉页脚、批注忽略掉，先保证主文本可用。

### PPTX

提取内容：

- 幻灯片标题
- 文本框正文
- 备注区文本

建议处理原则：

- 以“单页幻灯片”为天然结构边界
- 保留页序号，便于 chunk 审核和重排
- 图片本体先不解析，只保留图注或相邻说明文本

### Markdown

提取内容：

- 标题层级
- 段落
- 列表
- 代码块
- 表格

建议处理原则：

- 保留 Markdown 标题结构
- 代码块作为独立片段处理，避免与正文混切
- 图片语法先保留 `alt` 文本和链接占位

第一版解析边界建议：

- `pdf`：通过 Python `pdfplumber` 提取可提取文本的 PDF，并尝试将页内表格转成 Markdown 表格文本
- 扫描版 PDF：直接失败，不做 OCR
- `docx`：解析正文、标题、表格；表格统一转成 Markdown 表格文本，相邻且列数一致的连续表会按续表合并并去掉重复表头，忽略图片内容识别、批注、页眉页脚
- `pptx`：解析标题、文本框、备注区；原生表格统一转成 Markdown 表格文本，图片内容不做识别
- `markdown`：解析标题、段落、列表、代码块、表格和链接；原有 Markdown 表格语法保持原样

## 文本清洗

建议做这几步：

- 统一空白字符
- 去掉连续空行
- 去掉重复页眉页脚
- 合并被错误断开的句子
- 保留标题结构

注意：

- 不要过度清洗，避免破坏原文语义
- 表格先转成按行拼接的文本，后续再考虑结构化表格检索
- 当前实现里 `docx/pptx` 直接转成 Markdown 表格文本；`pdf` 使用 `pdfplumber` 提取普通文本并尝试抽取页内表格，正文会尽量排除表格区域以减少重复，并会合并相邻页间列数一致的续页表，但复杂跨页表或扫描版仍可能退化或失败

## Chunk 切分

建议使用“结构优先 + 长度兜底”的混合策略。

### 切分原则

先按这些边界切：

- 标题
- 段落
- 页面

再按 token 长度兜底切分。

### 推荐参数

- chunk 大小：`400 ~ 800 tokens`
- chunk overlap：`50 ~ 120 tokens`

如果现在没有稳定 token 计数器，最小实现可以先按字符数近似：

- `800 ~ 1200` 中文字符
- overlap `100 ~ 200` 字符

第一版默认参数建议固定为：

- `chunk_size = 1000` 字符
- `chunk_overlap = 150` 字符

短文档规则建议：

- 如果清洗后的正文总长度不超过约 `3000` 中文字符，可默认不拆分，整篇作为一个 chunk
- 如果总长度不大，但存在明显结构边界，例如独立章节、超长代码块、超大表格，仍可按结构拆分
- 即使不拆分，也保留 `resource_refs`

chunk 落盘和快照复用的具体实现约定，见 [后端架构与技术方案](./backend.md)。

### 推荐 chunk 内容

每个 chunk 尽量带上少量结构上下文，例如：

```text
文档标题: 员工手册
章节: 请假制度
正文: ...
```

## Chunk 人工审核与重排

如果业务希望确保入库质量，建议把 chunk 处理拆成两个阶段：

1. `chunk draft`
2. `chunk review`

只有审核通过后的 chunks，才进入 embedding 和 Milvus。

### 审核能力（已实现）

| 操作 | 说明 |
|---|---|
| 查看 chunk 列表 | 含页码、章节、序号、原文、清洗后文本 |
| reject | 标记 chunk 忽略入库（`is_current=false, status=rejected`） |
| restore | 将已 rejected 的 chunk 恢复为 draft |
| edit | 编辑 chunk 正文（`source=manual`） |
| merge | 合并相邻 chunk（后端校验 `chunk_index` 连续性） |
| approve | 全部 draft chunk → approved，自动触发 embedding + indexing |
| rechunk | 重新自动切分整个文档，生成新的 chunk version |

### 审核后的状态

- `draft`
- `approved`
- `rejected`

文档级状态补充：

- `review_pending`：切分完成等待审核
- `approved`：审核通过，已进入 embedding 流程（审核通过后自动触发）

如果未开启人工审核（`human_review=false`），则自动切分完成后直接进入 embedding 和 Milvus 入库，以上审核操作不适用。

## 删除与更新

### 删除文档

需要同时删除：

- 本地存储中的原文件
- 本地 chunk JSON 快照
- 本地派生资源文件
- `documents` 记录
- `document_chunks` 记录
- Milvus 中 `document_id = ?` 的所有向量

### 更新文档

建议流程：

1. 标记旧版本为 `reindexing`
2. 删除旧 chunk 和旧向量
3. 重新走解析、切块流程
4. 先进入 `review_pending`
5. 审核通过后再做 embedding、写入流程

## 检索时的配套约束

- 先按 `knowledge_base_id` 过滤
- topK 召回 chunk
- 按 `document_id` 或相邻 `chunk_index` 做合并

## 错误处理

- 文件类型不支持
- 文件损坏
- PDF 无可提取文本
- DOCX 解析失败
- chunk 为空
- embedding 失败
- Milvus 写入失败

每一步都要把错误写回 `documents.error_message`。
