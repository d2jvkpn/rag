# RAG 文档处理方案

## 文档结构

当前方案拆分为 4 份文档：

- [业务方案](./business.md)
- [架构与技术方案](./architecture.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)

## 范围

第一版先只支持两类输入文档：

- `pdf`
- `docx`

处理链路限定为：

1. 接收文档上传
2. 提取文本和基础元数据
3. 按规则切分为 chunks
4. 用户人工审核 chunks
5. 生成向量
6. 写入 Milvus
7. 保存入库状态，支持失败重试

## 一句话结论

先做一个 `Gin + Asynq + PostgreSQL + Milvus` 的异步入库链路，围绕 `pdf/docx -> text -> chunk draft -> 人工审核 -> embedding api -> Milvus` 建最小闭环；原文存本地文件系统，数据状态放关系库，向量放 Milvus，PDF 解析复用 Python 生态，第一版不要引入 OCR 和增量更新。
