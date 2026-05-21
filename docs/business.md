# RAG 文档处理业务方案

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
5. 用户人工审核 chunks
6. 生成 embedding
7. 写入 Milvus
8. 提供删除、重建、状态查询

## 上传阶段

输入：

- 文件二进制
- `knowledge_base_id`
- 可选：`doc_id`、`title`、`tags`

`knowledge_base_id` 的作用不是“附带信息”，而是文档归属边界。它至少承担 4 个职责：

- 标识文档属于哪个知识库
- 作为检索时的过滤条件，避免跨知识库召回
- 作为去重边界的一部分，例如 `knowledge_base_id + sha256`
- 作为删除范围的一部分，避免误删其他知识库的向量

如果系统只有单一知识库，也建议保留这个字段。这样后面扩展多租户、多业务空间时，不需要重做 schema。

落盘建议：

- 原文件存本地文件目录
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

### 审核能力建议

- 查看 chunk 列表
- 查看 chunk 对应的页码、章节、序号
- 查看 chunk 原文和清洗后文本
- 合并相邻 chunks
- 拆分某个 chunk
- 调整 chunk 顺序
- 删除噪声 chunk
- 标记某个 chunk 为“忽略入库”
- 重新触发自动切分

### 审核后的状态

- `draft`
- `approved`
- `rejected`

文档级状态建议补充：

- `review_pending`
- `reviewing`
- `approved`

### 第一版取舍

第一版可以只支持：

- 查看 chunk 列表
- 删除 chunk
- 合并相邻 chunk
- 重新切分整个文档
- 审核通过后再入库

## 删除与更新

### 删除文档

需要同时删除：

- 本地存储中的原文件
- `documents` 记录
- `document_chunks` 记录
- Milvus 中 `document_id = ?` 的所有向量

### 更新文档

建议流程：

1. 标记旧版本为 `reindexing`
2. 删除旧 chunk 和旧向量
3. 重新走解析、切块流程
4. 如果启用人工审核，则先进入 `review_pending`
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
