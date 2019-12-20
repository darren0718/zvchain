package core

import (
	"bytes"
	"fmt"
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/storage/tasdb"
	"sync"
)

var (
	persistentHeight = "ph"
	smallDbRootDatas = "dt"
	lastDeleteHeight = "ldt"
)

type smallStateStore struct {
	db tasdb.Database
	mu sync.Mutex // Mutex lock
}

func initSmallStore(db tasdb.Database) *smallStateStore {
	return &smallStateStore{
		db: db,
	}
}

func (store *smallStateStore) GetLastDeleteHeight() uint64 {
	data, _ := store.db.Get([]byte(lastDeleteHeight))
	return common.ByteToUInt64(data)
}

func (store *smallStateStore) GetSmallDbDatasByRoot(root common.Hash) []byte {
	data, _ := store.db.Get(store.generateKey(root[:], smallDbRootDatas))
	return data
}

func (store *smallStateStore) DeleteSmallDbDatasByRoot(root common.Hash) error {
	err := store.db.Delete(store.generateKey(root[:], smallDbRootDatas))
	if err != nil {
		return fmt.Errorf("delete dirty trie error %v", err)
	}
	return nil
}

func (store *smallStateStore) DeleteSmallDbDataByRoot(root common.Hash, height uint64) error {
	err := store.db.Delete(store.generateKey(root[:], smallDbRootDatas))
	if err != nil {
		return fmt.Errorf("delete state data from small db error %v", err)
	}
	err = store.db.Put([]byte(lastDeleteHeight), common.UInt64ToByte(height))
	if err != nil {
		return fmt.Errorf("put last delete height error %v,height is %v", err, height)
	}
	return nil
}

func (store *smallStateStore) StoreDataToSmallDb(root common.Hash, nb []byte) error {
	err := store.db.Put(store.generateKey(root[:], smallDbRootDatas), nb)
	if err != nil {
		return fmt.Errorf("store state data to small db error %v", err)
	}
	return nil
}

func (store *smallStateStore) StoreStatePersistentHeight(height uint64) error {
	err := store.db.Put([]byte(persistentHeight), common.UInt64ToByte(height))
	if err != nil {
		return fmt.Errorf("store trie pure copy info error %v", err)
	}
	return nil
}

func (store *smallStateStore) GetStatePersistentHeight() uint64 {
	data, _ := store.db.Get([]byte(persistentHeight))
	return common.ByteToUInt64(data)
}

// generateKey generate a prefixed key
func (store *smallStateStore) generateKey(raw []byte, prefix string) []byte {
	bytesBuffer := bytes.NewBuffer([]byte(prefix))
	if raw != nil {
		bytesBuffer.Write(raw)
	}
	return bytesBuffer.Bytes()
}

func (store *smallStateStore) Close() {
	if store.db != nil {
		store.db.Close()
	}
}
