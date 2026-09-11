//go:build e2e

// farm-service and file-service.
//
// A farm is a place with sections and a capacity; a file record is a pointer at
// something in object storage. Neither computes anything, so the assertions are
// the ones that matter for services shaped like this: an update that changes
// only what it was asked to, a list scoped to the thing it was asked about, and
// a delete that stops the thing being handed out.
package e2e

import (
	"context"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	farmSvc = "farm.v1.FarmService"
	fileSvc = "file.v1.FileService"
)

type updateFarmReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	City      string `json:"city"`
	State     string `json:"state"`
	Country   string `json:"country"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
	UpdatedBy string `json:"updated_by"`
}

type updateFarmCapacityReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Capacity  int    `json:"capacity"`
	UpdatedBy string `json:"updated_by"`
}

type createFarmSectionReq struct {
	TenantID         string `json:"tenant_id"`
	FarmID           string `json:"farm_id"`
	Name             string `json:"name"`
	SectionType      string `json:"section_type"`
	Capacity         int    `json:"capacity"`
	CurrentOccupancy int    `json:"current_occupancy"`
	CreatedBy        string `json:"created_by"`
}

type farmSectionResp struct {
	Section *struct {
		ID       string `json:"id"`
		FarmID   string `json:"farm_id"`
		Name     string `json:"name"`
		Capacity int    `json:"capacity"`
	} `json:"section"`
}

type listFarmSectionsReq struct {
	TenantID string `json:"tenant_id"`
	FarmID   string `json:"farm_id"`
}

type listFarmSectionsResp struct {
	Sections []*struct {
		ID     string `json:"id"`
		FarmID string `json:"farm_id"`
	} `json:"sections"`
}

func aFarm(t *testing.T, p *platform) *farmResp {
	t.Helper()
	code := newID("frm")
	out, err := svcclient.Call[createFarmReq, farmResp](
		context.Background(), p.farm(), farmSvc+"/CreateFarm",
		createFarmReq{TenantID: p.tenant, Name: "Kothapalli dairy", Code: code,
			Address: "village road", Country: "IN", Capacity: 120,
			Status: "active", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create farm: %v", err)
	}
	return out
}

// A farm reads back, and an update changes what it was asked to and not the
// capacity.
//
// Capacity has its own endpoint, which is the interesting part: an update that
// also reset it to whatever the caller happened to leave at zero would empty a
// farm's stated capacity every time somebody corrected its address.
func TestUpdatingAFarmLeavesItsCapacityAlone(t *testing.T) {
	p := startPlatform(t)
	made := aFarm(t, p)

	got, err := svcclient.Call[idTenantReq, farmResp](
		context.Background(), p.farm(), farmSvc+"/GetFarm",
		idTenantReq{ID: made.Farm.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get farm: %v", err)
	}
	if got.Farm.Capacity != 120 {
		t.Fatalf("the farm reads back with capacity %d, want 120", got.Farm.Capacity)
	}

	updated, err := svcclient.Call[updateFarmReq, farmResp](
		context.Background(), p.farm(), farmSvc+"/UpdateFarm",
		updateFarmReq{ID: made.Farm.ID, TenantID: p.tenant,
			Name: "Kothapalli dairy, east", Address: "village road",
			City: "Kothapalli", Country: "IN", Status: "active",
			UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("update farm: %v", err)
	}
	if updated.Farm.City != "Kothapalli" {
		t.Errorf("the city reads %q after being set", updated.Farm.City)
	}
	if updated.Farm.Capacity != 120 {
		t.Errorf("correcting the address changed the capacity from 120 to %d; "+
			"capacity has its own endpoint and an update that carries none should "+
			"not be read as one that sets it to nothing", updated.Farm.Capacity)
	}

	// And the endpoint that is for it does change it.
	bigger, err := svcclient.Call[updateFarmCapacityReq, farmResp](
		context.Background(), p.farm(), farmSvc+"/UpdateFarmCapacity",
		updateFarmCapacityReq{ID: made.Farm.ID, TenantID: p.tenant,
			Capacity: 180, UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("update capacity: %v", err)
	}
	if bigger.Farm.Capacity != 180 {
		t.Errorf("capacity = %d after being set to 180", bigger.Farm.Capacity)
	}
}

// A section belongs to the farm it was created against.
func TestAFarmSectionBelongsToItsFarm(t *testing.T) {
	p := startPlatform(t)
	mine, other := aFarm(t, p), aFarm(t, p)

	for _, f := range []string{mine.Farm.ID, mine.Farm.ID, other.Farm.ID} {
		if _, err := svcclient.Call[createFarmSectionReq, farmSectionResp](
			context.Background(), p.farm(), farmSvc+"/CreateFarmSection",
			createFarmSectionReq{TenantID: p.tenant, FarmID: f,
				Name: newID("sec"), SectionType: "milking", Capacity: 40,
				CurrentOccupancy: 0, CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("create section: %v", err)
		}
	}

	list, err := svcclient.Call[listFarmSectionsReq, listFarmSectionsResp](
		context.Background(), p.farm(), farmSvc+"/ListFarmSections",
		listFarmSectionsReq{TenantID: p.tenant, FarmID: mine.Farm.ID}, p.opts())
	if err != nil {
		t.Fatalf("list sections: %v", err)
	}
	if len(list.Sections) != 2 {
		t.Fatalf("the farm holds %d sections, want 2 — another farm has one and it is "+
			"not this farm's", len(list.Sections))
	}
	for _, s := range list.Sections {
		if s.FarmID != mine.Farm.ID {
			t.Errorf("a section of farm %s came back in farm %s's list",
				s.FarmID, mine.Farm.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// file
// ---------------------------------------------------------------------------

type createFileRecordReq struct {
	TenantID     string `json:"tenant_id"`
	OriginalName string `json:"original_name"`
	StoredName   string `json:"stored_name"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	StoragePath  string `json:"storage_path"`
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	UploadedBy   string `json:"uploaded_by"`
	IsPublic     bool   `json:"is_public"`
	CreatedBy    string `json:"created_by"`
}

type fileRecordResp struct {
	File *struct {
		ID           string `json:"id"`
		OriginalName string `json:"original_name"`
		EntityType   string `json:"entity_type"`
		EntityID     string `json:"entity_id"`
		SizeBytes    int64  `json:"size_bytes"`
	} `json:"file"`
}

type listEntityFilesReq struct {
	TenantID   string `json:"tenant_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

type listEntityFilesResp struct {
	Files []*struct {
		ID       string `json:"id"`
		EntityID string `json:"entity_id"`
	} `json:"files"`
}

type deleteFileReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

func aFileFor(t *testing.T, p *platform, entityType, entityID string) *fileRecordResp {
	t.Helper()
	name := newID("doc") + ".pdf"
	out, err := svcclient.Call[createFileRecordReq, fileRecordResp](
		context.Background(), p.file(), fileSvc+"/CreateFileRecord",
		createFileRecordReq{TenantID: p.tenant, OriginalName: name,
			StoredName: name, ContentType: "application/pdf", SizeBytes: 4096,
			StoragePath: "/files/" + name, EntityType: entityType, EntityID: entityID,
			UploadedBy: "e2e", IsPublic: false, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create file record: %v", err)
	}
	return out
}

// A file record is attached to one thing, and listed against that thing.
//
// A list that ignores the entity hands somebody every document the tenant holds
// when they asked for one animal's certificates.
func TestAFileRecordIsAttachedToOneThing(t *testing.T) {
	p := startPlatform(t)
	mine, other := newID("cow"), newID("cow")

	made := aFileFor(t, p, "cattle", mine)
	aFileFor(t, p, "cattle", mine)
	aFileFor(t, p, "cattle", other)

	got, err := svcclient.Call[idTenantReq, fileRecordResp](
		context.Background(), p.file(), fileSvc+"/GetFileRecord",
		idTenantReq{ID: made.File.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get file record: %v", err)
	}
	if got.File.SizeBytes != 4096 || got.File.EntityID != mine {
		t.Errorf("the record reads back as %d bytes against %s",
			got.File.SizeBytes, got.File.EntityID)
	}

	list, err := svcclient.Call[listEntityFilesReq, listEntityFilesResp](
		context.Background(), p.file(), fileSvc+"/ListEntityFiles",
		listEntityFilesReq{TenantID: p.tenant, EntityType: "cattle", EntityID: mine},
		p.opts())
	if err != nil {
		t.Fatalf("list entity files: %v", err)
	}
	if len(list.Files) != 2 {
		t.Fatalf("one animal holds %d files, want 2 — another animal has one and it "+
			"is not this animal's", len(list.Files))
	}
	for _, f := range list.Files {
		if f.EntityID != mine {
			t.Errorf("a file for %s came back against %s", f.EntityID, mine)
		}
	}
}

// A deleted file stops being handed out.
//
// The record is soft-deleted, so the row survives — but a download URL for it
// must not, or deleting a document removes it from every list and leaves the
// link working.
func TestADeletedFileStopsBeingHandedOut(t *testing.T) {
	p := startPlatform(t)
	entity := newID("cow")
	made := aFileFor(t, p, "cattle", entity)

	if _, err := svcclient.Call[idTenantReq, downloadURLResp](
		context.Background(), p.file(), fileSvc+"/GetDownloadURL",
		idTenantReq{ID: made.File.ID, TenantID: p.tenant}, p.opts()); err != nil {
		t.Fatalf("a live file has no download url: %v", err)
	}

	if _, err := svcclient.Call[deleteFileReq, struct{}](
		context.Background(), p.file(), fileSvc+"/DeleteFile",
		deleteFileReq{ID: made.File.ID, TenantID: p.tenant, UpdatedBy: "e2e"},
		p.opts()); err != nil {
		t.Fatalf("delete file: %v", err)
	}

	if _, err := svcclient.Call[idTenantReq, downloadURLResp](
		context.Background(), p.file(), fileSvc+"/GetDownloadURL",
		idTenantReq{ID: made.File.ID, TenantID: p.tenant}, p.opts()); err == nil {
		t.Error("a deleted file still has a download url; it is gone from every list " +
			"and the link still works")
	}

	list, err := svcclient.Call[listEntityFilesReq, listEntityFilesResp](
		context.Background(), p.file(), fileSvc+"/ListEntityFiles",
		listEntityFilesReq{TenantID: p.tenant, EntityType: "cattle", EntityID: entity},
		p.opts())
	if err != nil {
		t.Fatalf("list entity files: %v", err)
	}
	for _, f := range list.Files {
		if f.ID == made.File.ID {
			t.Error("a deleted file is still listed")
		}
	}
}
