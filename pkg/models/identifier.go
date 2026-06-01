package models

import (
	pb "p9e.in/samavaya/packages/api/v1/identifier"
)

type Identifier struct {
	Id   int64
	Uuid string
}

// ProtoToIdentifier converts a proto UserIdentifier to a model UserIdentifier
func ProtoToUserIdentifier(pi *pb.Identifier) *Identifier {
	if pi == nil {
		return nil
	}

	var id int64
	var uuid string

	switch x := pi.Identifier.(type) {
	case *pb.Identifier_Id:
		id = x.Id
	case *pb.Identifier_Uuid:
		uuid = x.Uuid
	default:
		// No identifier provided
		return nil
	}

	return &Identifier{
		Id:   id,
		Uuid: uuid,
	}
}

// IdentifierToProto converts a model UserIdentifier to a proto UserIdentifier
func UserIdentifierToProto(mi *Identifier) *pb.Identifier {
	if mi == nil {
		return nil
	}

	switch {
	case mi.Id != 0:
		return &pb.Identifier{
			Identifier: &pb.Identifier_Id{Id: mi.Id},
		}
	case mi.Uuid != "":
		return &pb.Identifier{
			Identifier: &pb.Identifier_Uuid{Uuid: mi.Uuid},
		}
	default:
		return nil
	}
}
