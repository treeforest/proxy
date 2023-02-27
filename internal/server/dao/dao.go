package dao

import (
	bolt "go.etcd.io/bbolt"
)

type Dao struct {
	db *bolt.DB
}

func New(path string) *Dao {
	db, err := bolt.Open(path, 0666, nil)
	if err != nil {
		panic(err)
	}
	d := &Dao{
		db: db,
	}
	d.createBuckets()
	return d
}

func (d *Dao) Close() {
	_ = d.db.Close()
}
