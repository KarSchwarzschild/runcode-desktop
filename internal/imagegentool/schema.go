package imagegentool

import "gitlab.ouc-online.com.cn/aibase/agentloop/tool"

// Name is the model-facing tool id and permission class key.
const Name = "image_generate"

// BindingKind names the authorization target family: the platform Bridge's
// image generation route, pinned per frozen turn destination.
const BindingKind = "platform-image-generation"

const (
	// MaxPromptRunes bounds the trimmed prompt (design §5).
	MaxPromptRunes = 2000
	// MaxResultBytes bounds the JSON shared by tool content and UI data.
	MaxResultBytes  = 8 * 1024
	maxMessageRunes = 400
	// maxRememberedTurns bounds the per-tool replay/budget memory. The host's
	// persistent ledger, not this cache, is the cross-process authority.
	maxRememberedTurns = 64
)

// InputSchema is the frozen JSON schema advertised to the model. Only the
// prompt and an optional size are model-controlled; model id, tenant, user,
// count, credentials, gateway and output path all belong to the host.
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
