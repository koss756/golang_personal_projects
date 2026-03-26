package kvstore

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
)

// ErrKeyNotFound is returned for GET when the key is absent (leader-only reads).
var ErrKeyNotFound = errors.New("key not found")

const (
	OpSet    = "set"
	OpDelete = "delete"
)

// Command is replicated through Raft (set / delete only).
type Command struct {
	Op    string
	Key   string
	Value string
}

type KVStore struct {
	data map[string]string
}

func NewKVStore() *KVStore {
	return &KVStore{
		data: make(map[string]string),
	}
}

func EncodeCommand(cmd Command) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cmd); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeCommand(b []byte) (Command, error) {
	var cmd Command
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&cmd); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

// Apply executes a committed log entry (gob-encoded Command).
func (kv *KVStore) Apply(encoded []byte) error {
	if len(encoded) == 0 {
		return nil
	}
	cmd, err := DecodeCommand(encoded)
	if err != nil {
		return err
	}
	switch cmd.Op {
	case OpSet:
		kv.data[cmd.Key] = cmd.Value
		return nil
	case OpDelete:
		delete(kv.data, cmd.Key)
		return nil
	default:
		return fmt.Errorf("kvstore: unknown op %q", cmd.Op)
	}
}

// Get returns the current value for key (leader-local read; not linearizable across followers).
func (kv *KVStore) Get(key string) (string, bool) {
	v, ok := kv.data[key]
	return v, ok
}

func (kv *KVStore) GetAll() map[string]string {
	copied := make(map[string]string, len(kv.data))
	for k, v := range kv.data {
		copied[k] = v
	}
	return copied
}
