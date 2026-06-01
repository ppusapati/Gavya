package onboarding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"p9e.in/samavaya/packages/classregistry"
	"p9e.in/samavaya/packages/errors"
)

// Loader reads industry-profile YAML files from a directory. The
// directory layout is fixed:
//
//	<root>/<industry_code>.yaml
//
// One file per industry. The filename (minus `.yaml`) must match the
// document's `industry_code:` field — same filename/content binding
// rule the classregistry loader uses, for the same reason: drift
// between file path and content is the kind of bug that silently
// ships the wrong config.
type Loader struct {
	// Root is the directory containing per-industry YAML files.
	Root string

	// Registry is required: the loader validates that every
	// EnabledClass in every profile resolves to a real class in the
	// registry. A profile that references an undeclared class fails
	// load-time validation rather than blowing up at provisioning
	// time against a live tenant.
	Registry classregistry.Registry
}

// NewLoader constructs a Loader.
func NewLoader(root string, reg classregistry.Registry) *Loader {
	return &Loader{Root: root, Registry: reg}
}

// Load reads every `*.yaml` file under Root and returns a map keyed
// by IndustryCode. An absent directory is treated as zero profiles —
// not an error — so deployments that predate F.6.3's first profile
// still start.
func (l *Loader) Load() (map[string]*Profile, error) {
	if l.Registry == nil {
		return nil, errors.BadRequest(
			"ONBOARDING_LOADER_NO_REGISTRY",
			"class registry is required for profile validation",
		)
	}
	info, err := os.Stat(l.Root)
	if os.IsNotExist(err) {
		return map[string]*Profile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", l.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", l.Root)
	}

	entries, err := os.ReadDir(l.Root)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", l.Root, err)
	}

	out := map[string]*Profile{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(l.Root, name)
		p, err := l.loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		if _, dup := out[p.IndustryCode]; dup {
			return nil, fmt.Errorf("duplicate industry_code %q (found in %s)", p.IndustryCode, path)
		}
		out[p.IndustryCode] = p
	}
	return out, nil
}

// LoadBytes is the test-friendly variant. The map key is the expected
// industry_code; the value is the raw YAML. Same validation runs.
func (l *Loader) LoadBytes(contents map[string][]byte) (map[string]*Profile, error) {
	if l.Registry == nil {
		return nil, errors.BadRequest(
			"ONBOARDING_LOADER_NO_REGISTRY",
			"class registry is required for profile validation",
		)
	}
	out := map[string]*Profile{}
	for industry, raw := range contents {
		p, err := l.parseBytes(industry, raw)
		if err != nil {
			return nil, err
		}
		out[p.IndustryCode] = p
	}
	return out, nil
}

func (l *Loader) loadFile(path string) (*Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	inferred := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	return l.parseBytes(inferred, raw)
}

func (l *Loader) parseBytes(inferred string, raw []byte) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse yaml for %q: %w", inferred, err)
	}
	if p.IndustryCode == "" {
		p.IndustryCode = inferred
	}
	if p.IndustryCode != inferred && inferred != "" {
		return nil, fmt.Errorf("industry_code mismatch in %q.yaml: file implies %q but document declares %q",
			inferred, inferred, p.IndustryCode)
	}
	if err := l.validate(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (l *Loader) validate(p *Profile) error {
	if p.Label == "" {
		return errors.BadRequest(
			"ONBOARDING_PROFILE_MISSING_LABEL",
			fmt.Sprintf("industry %q: label is required", p.IndustryCode),
		)
	}
	if p.Version == "" {
		return errors.BadRequest(
			"ONBOARDING_PROFILE_MISSING_VERSION",
			fmt.Sprintf("industry %q: version is required", p.IndustryCode),
		)
	}

	seen := map[string]bool{}
	for i, ec := range p.EnabledClasses {
		if ec.Domain == "" || ec.Class == "" {
			return errors.BadRequest(
				"ONBOARDING_PROFILE_INVALID_ENABLED_CLASS",
				fmt.Sprintf("industry %q: enabled_classes[%d] missing domain or class", p.IndustryCode, i),
			)
		}
		key := ec.Domain + "/" + ec.Class
		if seen[key] {
			return errors.BadRequest(
				"ONBOARDING_PROFILE_DUPLICATE_CLASS",
				fmt.Sprintf("industry %q: enabled_classes[%d] duplicates %q", p.IndustryCode, i, key),
			)
		}
		seen[key] = true

		// Existence check: the registry must know this class.
		if _, err := l.Registry.GetClass(ec.Domain, ec.Class); err != nil {
			return errors.BadRequest(
				"ONBOARDING_PROFILE_UNKNOWN_CLASS",
				fmt.Sprintf("industry %q: enabled_classes[%d] references unknown class %q: %v",
					p.IndustryCode, i, key, err),
			)
		}
	}

	for i, sm := range p.SeedMasters {
		if sm.Domain == "" || sm.Table == "" {
			return errors.BadRequest(
				"ONBOARDING_PROFILE_INVALID_SEED_MASTER",
				fmt.Sprintf("industry %q: seed_masters[%d] missing domain or table", p.IndustryCode, i),
			)
		}
		for ri, row := range sm.Rows {
			if len(row) == 0 {
				return errors.BadRequest(
					"ONBOARDING_PROFILE_EMPTY_SEED_ROW",
					fmt.Sprintf("industry %q: seed_masters[%d].rows[%d] is empty", p.IndustryCode, i, ri),
				)
			}
		}
	}

	return nil
}
