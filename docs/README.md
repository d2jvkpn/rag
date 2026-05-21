# RAG 文档处理方案

## 文档结构

当前方案拆分为 6 份文档：

- [流程与业务规则](./workflow.md)
- [后端架构与技术方案](./backend.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)
- [前端业务设计](./frontend-business.md)
- [前端技术方案](./frontend.md)

## 范围

第一版支持四类输入文档：

- `pdf`
- `docx`
- `pptx`
- `markdown`

## 一句话结论

第一版先实现一个面向 `pdf/docx/pptx/markdown` 的最小文档入库闭环：上传、解析、切分、可选人工审核、embedding 和向量写入；详细规则分别见流程、后端、API、数据模型和前端文档。
