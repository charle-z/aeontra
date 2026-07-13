package oauth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenStoreConcurrentSingleUseCodeAndRefresh(t *testing.T) {
	store := newTokenStore()
	store.putCode("code", authCode{clientID: "client", expiresAt: time.Now().Add(time.Minute)})
	store.putRefresh("refresh", refreshGrant{clientID: "client", expiresAt: time.Now().Add(time.Minute)})

	const workers = 64
	var codeSuccess atomic.Int64
	var refreshSuccess atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, ok := store.consumeCode("code"); ok {
				codeSuccess.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, ok := store.consumeRefresh("refresh"); ok {
				refreshSuccess.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := codeSuccess.Load(); got != 1 {
		t.Fatalf("successful code consumes = %d, want 1", got)
	}
	if got := refreshSuccess.Load(); got != 1 {
		t.Fatalf("successful refresh consumes = %d, want 1", got)
	}
}

func TestTokenStoreConcurrentAccessPutAndGet(t *testing.T) {
	store := newTokenStore()
	const workers = 128
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			token := time.Unix(int64(index+1), 0).UTC().Format(time.RFC3339Nano)
			store.putAccess(token, accessGrant{clientID: token, expiresAt: time.Now().Add(time.Minute)})
			grant, ok := store.getAccess(token)
			if !ok || grant.clientID != token {
				t.Errorf("token %d missing or mismatched", index)
			}
		}(i)
	}
	wg.Wait()
}
