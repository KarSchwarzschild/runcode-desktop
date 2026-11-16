package imagegentool

import "gitlab.ouc-online.com.cn/aibase/agentloop/tool"

// Name 是面向模型的工具 id，也是权限类键。
const Name = "image_generate"

// BindingKind 命名授权目标族：平台 Bridge 生图路由，按冻结回合目的地钉死。
const BindingKind = "platform-image-generation"

const (
	// MaxPromptRunes 限制去空白后的 prompt 字数（设计第 5 节）。
	MaxPromptRunes = 2000
	// MaxResultBytes 限制工具正文与 UI Data 共用的 JSON 体积。
	MaxResultBytes  = 8 * 1024
	maxMessageRunes = 400
	// maxRememberedTurns 限制进程内回放／预算缓存；跨进程权威是宿主持久账本，不是这里。
	maxRememberedTurns = 64
)

// InputSchema 是向模型公布的冻结 JSON schema。模型只控 prompt 与可选 size；
// 模型 id、租户、用户、张数、凭据、网关与落盘路径一律归宿主。
func InputSchema() tool.Schema {
	return tool.Schema{
		Type:                 tool.SchemaTypeObject,
		AdditionalProperties: false,
		Required:             []string{"prompt"},
		Properties: map[string]tool.Schema{
			"prompt": {
				Type:        tool.SchemaTypeString,
				Description: "交给生图模型的画面描述（去除首尾空白后 1—2000 字）。只写画面内容，不要写操作指令。",
			},
			"size": {
				Type:        tool.SchemaTypeString,
				Description: "可选：图片尺寸，形如 1024x1024，必须是工具说明中列出的尺寸之一；省略时使用设置中的默认尺寸。",
			},
		},
	}
}
