package desktop

import (
	"context"
	"errors"
	"net"
	"strings"
)

// 目录／详情失败类别：供缓存失效判断；不解析中文文案做分支。
const (
	knowledgeFailureAuthentication = "authentication"
	knowledgeFailureForbidden      = "forbidden"
	knowledgeFailureContext        = "context_changed"
	knowledgeFailureTransient      = "transient"
	knowledgeFailureConfiguration  = "configuration"
	knowledgeFailureNotFound       = "not_found"
)

func knowledgeFailureKind(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, errKnowledgeAuthentication):
		return knowledgeFailureAuthentication
	case errors.Is(err, errKnowledgeForbidden):
		return knowledgeFailureForbidden
	case errors.Is(err, errNoSession), errors.Is(err, errNotLoggedIn):
		return knowledgeFailureAuthentication
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return knowledgeFailureTransient
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return knowledgeFailureTransient
	}
	msg := err.Error()
	if strings.Contains(msg, "配置") || errors.Is(err, errKnowledgeUnavailable) && strings.Contains(msg, "尚未启用") {
		return knowledgeFailureConfiguration
	}
	if errors.Is(err, errKnowledgeUnavailable) {
		return knowledgeFailureTransient
	}
	return knowledgeFailureTransient
}

func knowledgeWireError(err error) error {
	kind := knowledgeFailureKind(err)
	if kind == "" {
		return wireError(err)
	}
	return wireError(err) // 保留原消息；类别经 KnowledgePage.FailureKind 下发。
}
