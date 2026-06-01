package models

type Entity interface {
	GetID() int64
	GetUUID() string
}
