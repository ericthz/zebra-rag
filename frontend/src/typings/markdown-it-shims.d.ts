// 为使 vue-markdown-shiki(vue-markdown-it) 依赖的类型检查通过而补充的模块声明。
// markdown-it-container 的 @types 仍依赖 @types/markdown-it <14，与项目 markdown-it v14 冲突，
// 故不安装其 @types，改用 any 声明兜底（该插件为 vue-markdown-shiki 内部使用）。
declare module 'markdown-it-container';
