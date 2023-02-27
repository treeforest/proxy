package dao

import (
	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

var (
	_bucketAccount = []byte("account")
)

func (d *Dao) createBuckets() {
	_ = d.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(_bucketAccount)
		if err != nil {
			panic(err)
		}
		return nil
	})
}

func (d *Dao) Register(account, password string) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(_bucketAccount)
		val := bucket.Get([]byte(account))
		if val != nil {
			return errors.New("the account has been registered")
		}
		return bucket.Put([]byte(account), []byte(password))
	})
}

func (d *Dao) Login(account, password string) error {
	return d.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(_bucketAccount)
		val := bucket.Get([]byte(account))
		if val == nil {
			return errors.New("the account does not exist")
		}
		if string(val) != password {
			return errors.New("incorrect password")
		}
		return nil
	})
}
