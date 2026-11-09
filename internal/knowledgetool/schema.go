package knowledgetool

import "gitlab.ouc-online.com.cn/aibase/agentloop/tool"

// Name is the model-facing tool id and permission class key.
const Name = "knowledge_search"

const (
	MaxQueryUTF16   = 4000
	MaxLibraries    = 5
	MaxCallsPerTurn = 3
	MaxHits         = 5
	MaxHitRunes     = 2000
	MaxTotalRunes   = 8000
	MaxTitleRunes   = 128
	MaxDescRunes    = 600
)

// InputSchema is the frozen JSON schema advertised to the model.
func InputSchema() tool.Schema {
	return tool.Schema{
		Type:                 tool.SchemaTypeObject,
		AdditionalProperties: false,
		Required:             []string{"query"},
		Properties: map[string]tool.Schema{
			"query": {
				Type:        tool.SchemaTypeString,
				Description: "检索用查询词（可与用户原问不同）；最多 4000 个 UTF-16 码元，宿主拒绝超长，不截断。",
			},
			"libraryIds": {
				Type:        tool.SchemaTypeArray,
				Description: "可选：本题已选库的非空子集（1—5 个 UUID）。省略则查询本题全部可用库；显式空数组为参数错误。",
				Items:       &tool.Schema{Type: tool.SchemaTypeString},
			},
		},
	}
}
