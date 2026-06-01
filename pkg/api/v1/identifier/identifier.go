package identifier

// Identifier represents a proto-generated identifier with oneof semantics.
type Identifier struct {
	Identifier isIdentifier
}

type isIdentifier interface {
	isIdentifier()
}

// Identifier_Id is the int64 ID variant.
type Identifier_Id struct {
	Id int64
}

func (*Identifier_Id) isIdentifier() {}

// Identifier_Uuid is the string UUID variant.
type Identifier_Uuid struct {
	Uuid string
}

func (*Identifier_Uuid) isIdentifier() {}
