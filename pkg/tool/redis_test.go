package tool

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestParseRedisURI(t *testing.T) {
	uri := "redis://localhost:6379"
	addr, password, database, err := ParseRedisURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(addr, password, database)
}

func TestRedisPing(t *testing.T) {
	server := miniredis.RunT(t)
	uri := "redis://" + server.Addr()
	addr, password, database, err := ParseRedisURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	err = RedisPing(addr, password, database)
	if err != nil {
		t.Fatal(err)
	}
}
