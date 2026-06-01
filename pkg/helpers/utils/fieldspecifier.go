package helpers_utils

import (
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type FieldSpecifier interface {
	IsFieldSpecifier()
}
type LocalFieldMask fieldmaskpb.FieldMask

type StringSlice []string

func (f *LocalFieldMask) IsFieldSpecifier() {}
func (s StringSlice) IsFieldSpecifier()     {}
