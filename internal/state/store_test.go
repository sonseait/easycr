package state

import (
	bolt "go.etcd.io/bbolt"
	"testing"
)

func TestStoreReopen(t *testing.T) {
	d := t.TempDir()
	s, err := Open(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte("settings")).Put([]byte("test"), []byte("value")) }); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(d)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.DB.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket([]byte("settings")).Get([]byte("test"))) != "value" {
			t.Error("value lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
