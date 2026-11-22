package app

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform"
)

// The adapters below serve one module's port from another module's facade
// that NewApplication constructs later (ADR-001 rule 4: cross-module reads
// and cache invalidation go through the owner's facade). They resolve the
// facade when a call runs, after assembly.

// subscriptionNetworkReads serves the subscription module's network read
// ports (the nodes of a plan scope, the preview nodes, a subscription's
// traffic logs) from the network facade.
type subscriptionNetworkReads struct{ srv *Application }

func (r subscriptionNetworkReads) ListEnabledNodesByScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error) {
	return r.srv.Network.ListEnabledNodesByScope(ctx, nodeIDs, tags)
}

func (r subscriptionNetworkReads) ListEnabledNodes(ctx context.Context, limit int) ([]*node.Node, error) {
	return r.srv.Network.ListEnabledNodes(ctx, limit)
}

func (r subscriptionNetworkReads) SubscriptionTrafficLogs(ctx context.Context, userID, subscribeID int64, page, size int) ([]*traffic.TrafficLog, int64, error) {
	return r.srv.Network.SubscriptionTrafficLogs(ctx, userID, subscribeID, page, size)
}

// identityServerCaches serves the identity module's node-cache invalidation
// for deleted and disabled accounts from the network facade.
type identityServerCaches struct{ srv *Application }

func (c identityServerCaches) ClearServerCachesByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) error {
	return c.srv.Network.ClearServerCachesByNodeScope(ctx, nodeIDs, tags)
}

// platformClientApplications serves the platform's public download page
// from the subscription facade, copying the fields the page shows into the
// platform-owned type.
type platformClientApplications struct{ srv *Application }

func (p platformClientApplications) ListClientApplications(ctx context.Context) ([]platform.ClientApplication, error) {
	apps, err := p.srv.Subscription.ClientApplications(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]platform.ClientApplication, 0, len(apps))
	for _, item := range apps {
		if item == nil {
			continue
		}
		list = append(list, platform.ClientApplication{
			Id:           item.Id,
			Name:         item.Name,
			Description:  item.Description,
			Icon:         item.Icon,
			Scheme:       item.Scheme,
			IsDefault:    item.IsDefault,
			DownloadLink: item.DownloadLink,
		})
	}
	return list, nil
}
