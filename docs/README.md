# RAG 文档处理方案

## 文档结构

当前方案拆分为以下设计文档，外加 `plans/` 下的实施计划文档：

- [系统架构总览](./Architecture.md)
- [设计决策与关键约定](./Design.md)
- [流程与业务规则](./workflow.md)
- [后端架构与技术方案](./backend.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)
- [UX 设计](./ux.md)
- [前端技术方案](./frontend.md)
- [实施计划](./plans/phase-1.md)

## 范围

第一版支持四类输入文档：

- `pdf`
- `docx`
- `pptx`
- `markdown`

## 一句话结论

第一版先实现一个面向 `pdf/docx/pptx/markdown` 的最小文档入库闭环：上传、解析、切分、可选人工审核、embedding 和向量写入；详细规则分别见流程、后端、API、数据模型和前端文档。
