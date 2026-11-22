package network

import (
	"github.com/perfect-panel/backend/internal/module/network/internal/edge"
	"github.com/perfect-panel/backend/internal/module/network/internal/serverapi"
)

// Accounts is the module's port onto the identity domain: the account gate
// of the edge manifest and the enabled owners of the node user lists. The
// composition root passes the identity facade.
type Accounts interface {
	edge.AccountStateReader
	serverapi.AccountReader
}
