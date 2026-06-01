package helpers_utils

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Apply field mask updates from `source` to `target`
func ApplyFieldMask(mask *fieldmaskpb.FieldMask, source, target proto.Message) {
	sourceValue := source.ProtoReflect()
	targetValue := target.ProtoReflect()

	for _, path := range mask.Paths {
		field := sourceValue.Descriptor().Fields().ByName(protoreflect.Name(path))
		if field == nil {
			continue
		}

		// Copy field value from source to target
		targetValue.Set(field, sourceValue.Get(field))
	}
}
