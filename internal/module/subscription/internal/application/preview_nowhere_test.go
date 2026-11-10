package application

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/module/network/entity/node"
	dto "github.com/perfect-panel/server/internal/module/subscription/contract"
	"github.com/perfect-panel/server/internal/module/subscription/entity/client"
	"github.com/perfect-panel/server/internal/repository"
)

type nowherePreviewClients struct{ repository.ClientRepo }

func (nowherePreviewClients) FindOne(context.Context, int64) (*client.SubscribeApplication, error) {
	return &client.SubscribeApplication{SubscribeTemplate: `{{ .UserInfo.NowhereKey }}|{{ (index .Proxies 0).Version }}|{{ (index .Proxies 0).Obfs }}`, OutputFormat: "text"}, nil
}

type nowherePreviewNodes struct{}

func (nowherePreviewNodes) ListEnabledNodes(context.Context, int) ([]*node.Node, error) {
	return []*node.Node{{Id: 1, Protocol: "nowhere", Server: &node.Server{Protocols: `[{"type":"nowhere","port":443,"version":1,"enable":true,"security":"tls","sni":"nw.example","cert_mode":"self","alpn":["now/1"]}]`}}}, nil
}
func TestPreviewNowhereUsesValidNW3Credential(t *testing.T) {
	service := NewService(Deps{Clients: nowherePreviewClients{}, Nodes: nowherePreviewNodes{}})
	result, err := service.PreviewSubscribeTemplate(t.Context(), &dto.PreviewSubscribeTemplateRequest{Id: 1})
	if err != nil || result.Template != "00000000000040008000000000000001|3|morph" {
		t.Fatalf("preview=%+v %v", result, err)
	}
}
