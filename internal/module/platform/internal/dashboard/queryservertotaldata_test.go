package dashboard

import (
	"encoding/json"
	"testing"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
)

// Distinct values so swapping the two identifiers cannot pass.
const (
	testSubscribeID int64 = 71
	testUserID      int64 = 42
)

func TestUserTrafficDataFromRankingKeepsBothIdentifiers(t *testing.T) {
	got := userTrafficDataFromRanking(readmodel.UserTrafficRanking{
		UserId:      testUserID,
		SubscribeId: testSubscribeID,
		Upload:      11,
		Download:    22,
		Total:       33,
	})
	if got.SID != testSubscribeID {
		t.Fatalf("SID = %d, want the user_subscribe id %d", got.SID, testSubscribeID)
	}
	if got.UID != testUserID {
		t.Fatalf("UID = %d, want the user id %d", got.UID, testUserID)
	}
	if got.Upload != 11 || got.Download != 22 {
		t.Fatalf("traffic was not carried over: %#v", got)
	}
}

func TestUserTrafficDataFromRankLogKeepsBothIdentifiers(t *testing.T) {
	got := userTrafficDataFromRankLog(log.UserTraffic{
		SubscribeId: testSubscribeID,
		UserId:      testUserID,
		Upload:      11,
		Download:    22,
		Total:       33,
	})
	if got.SID != testSubscribeID {
		t.Fatalf("SID = %d, want the user_subscribe id %d", got.SID, testSubscribeID)
	}
	if got.UID != testUserID {
		t.Fatalf("UID = %d, want the user id %d", got.UID, testUserID)
	}
}

// The console reads both keys, so neither may disappear from the payload.
func TestUserTrafficDataSerializesBothIdentifiers(t *testing.T) {
	body, err := json.Marshal(userTrafficDataFromRanking(readmodel.UserTrafficRanking{
		UserId:      testUserID,
		SubscribeId: testSubscribeID,
	}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded map[string]int64
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["sid"] != testSubscribeID {
		t.Fatalf("sid = %d, want %d", decoded["sid"], testSubscribeID)
	}
	if decoded["uid"] != testUserID {
		t.Fatalf("uid = %d, want %d", decoded["uid"], testUserID)
	}
}
