package classify

// taxonomy.go is the single source of truth (SSOT) for the controlled vocabulary
// defined in prototype/taxonomy.md. Both this package and internal/normalize MUST
// import these exported lists rather than redeclaring strings, so that "a category
// must exist in the taxonomy" (manual-dataset-schema.md validation rule 4) is
// enforced against ONE list. classify_test.go asserts every produced category is a
// member of Categories; normalize re-uses IsCategory/IsAudience for the same rule.
//
// Keep this small. Expand only when a real seed record cannot be classified
// (per prototype/taxonomy.md).

// Category slugs (prototype/taxonomy.md "categories"). Order is stable so callers
// that range over it (e.g. classification keyword passes) are deterministic.
const (
	CategoryAI               = "ai"                // AI infra, applications, enterprise/generative AI.
	CategoryHumanoidRobotics = "humanoid-robotics" // Humanoid + general robotics, automation, smart factory.
	CategoryBio              = "bio"               // Bio, biotech, pharma partnering, lab automation.
	CategoryMedicalDevices   = "medical-devices"   // Medical devices, diagnostics, hospital tech.
	CategoryDigitalHealth    = "digital-health"    // Digital health, rehabilitation tech, healthcare IT.
)

// Categories is the controlled category vocabulary (SSOT). Any category produced
// by Classify or accepted by normalize MUST be a member of this slice.
var Categories = []string{
	CategoryAI,
	CategoryHumanoidRobotics,
	CategoryBio,
	CategoryMedicalDevices,
	CategoryDigitalHealth,
}

// Audience slugs (prototype/taxonomy.md "audience").
const (
	AudienceB2B           = "b2b"           // Business / industry attendees.
	AudienceBuyers        = "buyers"        // Procurement, distributors, hospitals.
	AudienceInvestors     = "investors"     // VC, CVC, analysts.
	AudienceManufacturers = "manufacturers" // OEM/ODM, suppliers.
	AudienceStartups      = "startups"      // Startup founders / pavilions.
	AudiencePublic        = "public"        // Open to general public.
)

// Audiences is the controlled audience vocabulary (SSOT).
var Audiences = []string{
	AudienceB2B,
	AudienceBuyers,
	AudienceInvestors,
	AudienceManufacturers,
	AudienceStartups,
	AudiencePublic,
}

// categorySet and audienceSet back the IsCategory/IsAudience membership checks.
var (
	categorySet = sliceToSet(Categories)
	audienceSet = sliceToSet(Audiences)
)

func sliceToSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

// IsCategory reports whether c is a member of the controlled category vocabulary.
// normalize uses this for schema validation rule 4 (category ∈ taxonomy).
func IsCategory(c string) bool {
	_, ok := categorySet[c]
	return ok
}

// IsAudience reports whether a is a member of the controlled audience vocabulary.
func IsAudience(a string) bool {
	_, ok := audienceSet[a]
	return ok
}
