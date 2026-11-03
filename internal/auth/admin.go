package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"easycr/internal/state"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"
)

func EnsureBootstrap(s *state.Store, out io.Writer) error {
	var password string
	err := s.DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("settings"))
		if b == nil {
			return errors.New("settings bucket missing")
		}
		flag, hash, schema := b.Get([]byte("bootstrapped")), b.Get([]byte("admin_hash")), b.Get([]byte("schema_version"))
		if flag != nil || hash != nil || schema != nil {
			if string(flag) != "1" || len(hash) == 0 || string(schema) != "1" {
				return errors.New("invalid bootstrap state")
			}
			if _, err := bcrypt.Cost(hash); err != nil {
				return fmt.Errorf("invalid admin hash: %w", err)
			}
			return nil
		}
		random := make([]byte, 24)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		password = base64.RawURLEncoding.EncodeToString(random)
		generated, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		for _, pair := range [][2][]byte{{[]byte("admin_hash"), generated}, {[]byte("schema_version"), []byte("1")}, {[]byte("bootstrapped"), []byte("1")}} {
			if err := b.Put(pair[0], pair[1]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if password != "" {
		_, err = fmt.Fprintf(out, "bootstrap_admin_password password=%s\n", password)
	}
	return err
}
