package desktop

// 客户端负责安全地显示结果，生成侧必须按任务语义选格式；
// 这里只声明格式分工，不复制工具执行和权限判断。
const renderRoutePrompt = `Content rendering:
- Follow an explicit output format requested by the user, while distinguishing a request to create that format from text that merely discusses it.
- For flowcharts, architecture diagrams, sequence diagrams, state diagrams, ER diagrams, gantt charts, and mind maps, output a Mermaid fenced code block directly. Do not create an HTML file only to preview Mermaid, and do not call image_generate for these diagrams.
- For precise vector graphics, geometry, technical illustrations, and static data charts, prefer SVG when SVG is a suitable output.
- For web pages, interactive interfaces, complex layouts, and dashboards, use HTML. The desktop chat preview is static and does not run model-generated scripts.
- Use image_generate only when the main requirement depends on realistic imagery, people, photography, complex textures, or artistic illustration. Do not use image generation for ordinary flowcharts, statistical charts, or diagrams whose text and geometry must be exact.`
