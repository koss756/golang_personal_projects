package kvstore

type KVStore struct {
	data map[string]string
}

func NewKVStore() *KVStore {
	return &KVStore{
		data: make(map[string]string),
	}
}

func (kv *KVStore) Apply(cmd Command) (string, error) {
	switch cmd.Op {
	case OpSet:
		kv.data[cmd.Key] = cmd.Value
		return "", nil

	case OpDelete:
		delete(kv.data, cmd.Key)
		return "", nil

	case OpGet:
		val, ok := kv.data[cmd.Key]
		if !ok {
			return "", fmt.Errorf("key not found")
		}
		return val, nil
	}

	return "", fmt.Errorf("unknown operation")
}
