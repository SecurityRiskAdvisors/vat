package vat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"sra/vat/internal/dao"

	"github.com/Khan/genqlient/graphql"
	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type RestoreOptionalParams struct {
	AssessmentName             string // Set desired assessment name to this one, if blank, use existing assessment name
	OverrideAssessmentTemplate bool   // Flag to override using the use of the existing template assessment. Directly import the tests instead (lower fidelty)
	DeleteOnFailure            bool   // Flag to delete assessments if the campaign import failed
	ForceEnvOnly               bool   // FLag to ignore template test cases even if one exists in the source
	// ResetGlobalId mints a new globalId for the assessment being restored
	// instead of reusing the one from the serialized data. VECTR rejects an
	// assessment create when its globalId already exists in the target
	// instance, which happens when the same source assessment is restored
	// into an instance that already holds a copy of it (e.g. under a
	// different name) -- set this to land it as an independent copy.
	ResetGlobalId bool
}

var ErrOrgNotFound = fmt.Errorf("could not find org(s)")
var ErrMissingLibraryAssessment = fmt.Errorf("missing library assessment")
var ErrInvalidAssessmentName = fmt.Errorf("assessment name override is invalid (blank?)")
var ErrAssessmentAlreadyExists = fmt.Errorf("assessment already exists")
var ErrCampaignNotFound = fmt.Errorf("campaign not found")
var ErrDuplicateGlobalId = fmt.Errorf("assessment globalId already exists in target instance, retry with --reset-id")

// ErrIncompleteDefenseToolData is returned when a DefenseToolRef is missing
// a piece of information reconcileDefenseTools needs to safely match or
// create it -- a blank tool name, product ref, product name, or layer name.
// This can come from a source VECTR instance that genuinely has incomplete
// data, or from a serialized file that predates this field being captured
// (an older vat version, or a legacy save). Either way, guessing at a value
// (e.g. inventing a name) would silently create hard-to-clean-up,
// empty-named tools/products/layers in the target instance, so restore
// stops instead.
var ErrIncompleteDefenseToolData = fmt.Errorf("defense tool data is incomplete")

// executorMap maps automation executor types (e.g., "powershell") to their corresponding internal representation.
// The read part of the API does not return an ENUM or fixed type, just a generic string. This maps it back
// to the object type
var executorMap map[string]dao.AttackAutomationExecutor = map[string]dao.AttackAutomationExecutor{
	"powershell":        dao.AttackAutomationExecutorPowershell,
	"inline_powershell": dao.AttackAutomationExecutorInlinePowershell,
	"command_prompt":    dao.AttackAutomationExecutorCmd,
	"sh":                dao.AttackAutomationExecutorSh,
	"bash":              dao.AttackAutomationExecutorBash,
	"":                  dao.AttackAutomationExecutorCmd,
}

// outcomeStatusMap maps test case outcome statuses (e.g., "Abandoned") to their corresponding internal representation.
// The read part of the API returns different values than the write part accepts
// This maps the two together
// Note -- it will always require a validation check before use
var outcomeStatusMap map[string]dao.TestCaseStatus = map[string]dao.TestCaseStatus{
	string(dao.TestCaseStatusAbandon):      dao.TestCaseStatusAbandon,
	"Abandoned":                            dao.TestCaseStatusAbandon,
	string(dao.TestCaseStatusNotperformed): dao.TestCaseStatusNotperformed,
	string(dao.TestCaseStatusCompleted):    dao.TestCaseStatusCompleted,
	string(dao.TestCaseStatusInprogress):   dao.TestCaseStatusInprogress,
	string(dao.TestCaseStatusPaused):       dao.TestCaseStatusPaused,
	"Not Performed":                        dao.TestCaseStatusNotperformed,
	"In Progress":                          dao.TestCaseStatusInprogress,
}

type AutomationArgumentTypes interface {
	dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignTestCasesTestCaseAutomationArgument | dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCaseAutomationArgument
}

type pointerAutomationArgs[A any] interface {
	*A
	AutomationArgumentFetcher
}

type AutomationArgumentFetcher interface {
	GetArgumentKey() string
	GetArgumentValue() string
	GetArgumentType() string
}

type Automator[A AutomationArgumentTypes] interface {
	GetAutomationCmd() string
	GetAutomationExecutor() string
	GetAutomationCleanup() string
	GetAutomationCleanupExecutor() string
	GetAutomationArgument() []A
}

func NewGroupedCreateTestCaseWithLibraryIdInput(db, campaignId string) *GroupedCreateTestCaseWithLibraryIdInput {
	return &GroupedCreateTestCaseWithLibraryIdInput{
		db:         db,
		campaignId: campaignId,
		groups:     make(map[string][]dao.CreateTestCaseDataWithLibraryIdInput),
	}
}

// GroupedCreateTestCaseWithLibraryIdInput splits queued test case inserts
// into batches, so a single CreateTestCasesByLibraryId request never contains
// the same libraryTestCaseId twice (VECTR rejects such a request).
type GroupedCreateTestCaseWithLibraryIdInput struct {
	db, campaignId string
	groups         map[string][]dao.CreateTestCaseDataWithLibraryIdInput
}

func (g *GroupedCreateTestCaseWithLibraryIdInput) Add(tcd dao.CreateTestCaseDataWithLibraryIdInput) {
	g.groups[tcd.LibraryTestCaseId] = append(g.groups[tcd.LibraryTestCaseId], tcd)
}

func (g *GroupedCreateTestCaseWithLibraryIdInput) Len() int {
	size := 0
	for _, tcs := range g.groups {
		size += len(tcs)
	}
	return size
}

// GenerateInsertsData spreads each libraryTestCaseId group's entries across
// batches by position, so batch i never contains two entries sharing a
// libraryTestCaseId.
func (g *GroupedCreateTestCaseWithLibraryIdInput) GenerateInsertsData() []dao.CreateTestCaseMatchByLibraryIdInput {
	maxSize := 0
	for _, entries := range g.groups {
		if len(entries) > maxSize {
			maxSize = len(entries)
		}
	}
	if maxSize == 0 {
		return nil
	}

	batches := make([]dao.CreateTestCaseMatchByLibraryIdInput, maxSize)
	for i := range batches {
		batches[i] = dao.CreateTestCaseMatchByLibraryIdInput{
			Db:                         g.db,
			CampaignId:                 g.campaignId,
			SuppressAutoTimelineEvents: true,
		}
	}
	for _, entries := range g.groups {
		for i, e := range entries {
			batches[i].CreateTestCaseInputs = append(batches[i].CreateTestCaseInputs, e)
		}
	}
	return batches
}

// validateRestorePrerequisites checks if organizations required for the
// assessment restore exist in the target VECTR instance. It returns a map
// of organization names to their VECTR objects, and an error if any
// organization is missing.
//
// Defense tools are handled separately, by reconcileDefenseTools below --
// unlike organizations, a missing tool is no longer terminal (VECTR can now
// create one), so that step has side effects and doesn't belong in a
// function named "validate".
func validateRestorePrerequisites(ctx context.Context, client graphql.Client, db string, orgMap map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization) (map[string]dao.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization, error) {
	slog.InfoContext(ctx, "Starting restore prerequisites validation",
		"db", db,
		"organization_count", len(orgMap),
	)

	// Check if the organizations are in the new instance, error if not
	missing_orgs := []string{}
	org_map := make(map[string]dao.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization)
	for o, om := range orgMap {
		r, err := dao.FindOrganization(ctx, client, o)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not fetch organization: %s, %s, %s, %s: %w", om.Name, om.Abbreviation, om.Description, om.Url, err)
		}
		if len(r.Organizations.Nodes) == 0 {
			missing_orgs = append(missing_orgs, o)
			continue
		}
		org_map[r.Organizations.Nodes[0].Name] = r.Organizations.Nodes[0]
	}
	slog.DebugContext(ctx, "Validating organizations",
		"total", len(orgMap),
		"missing_orgs", missing_orgs)
	if len(missing_orgs) > 0 {
		for _, org := range missing_orgs {
			om := orgMap[org]
			slog.ErrorContext(ctx, "missing organization", "name", om.Name, "abbreviation", om.Abbreviation, "desc", om.Description, "url", om.Url)
		}
		return nil, fmt.Errorf("these orgs are missing from your instance: %s: %w", strings.Join(missing_orgs, ","), ErrOrgNotFound)
	}

	return org_map, nil
}

// validateDefenseToolRefs checks every DefenseToolRef in toolsToReconcile
// for blank data reconcileDefenseTools cannot safely act on: a blank tool
// name, product ref, product name, or defense layer name. An empty Layers
// slice is fine (a tool can legitimately have none); a blank name *within*
// it is not. Vendor name is also allowed to be blank -- a tool's product
// may genuinely have no vendor. Returns a single joined error covering
// every offending ref, or nil if all is well.
func validateDefenseToolRefs(toolsToReconcile map[string]DefenseToolRef) error {
	var errs []error
	for key, ref := range toolsToReconcile {
		var problems []string
		if strings.TrimSpace(ref.Name) == "" {
			problems = append(problems, "tool name is blank")
		}
		if strings.TrimSpace(ref.Product.Ref) == "" {
			problems = append(problems, "product ref is blank")
		}
		if strings.TrimSpace(ref.Product.Name) == "" {
			problems = append(problems, "product name is blank")
		}
		for i, name := range ref.Layers {
			if strings.TrimSpace(name) == "" {
				problems = append(problems, fmt.Sprintf("layer #%d has a blank name", i))
			}
		}
		if len(problems) > 0 {
			errs = append(errs, fmt.Errorf("defense tool %q (key %q): %s: %w", ref.Name, key, strings.Join(problems, "; "), ErrIncompleteDefenseToolData))
		}
	}
	return errors.Join(errs...)
}

// reconcileDefenseTools ensures every DefenseToolRef in toolsToReconcile has
// a corresponding BlueTool in the target db: reusing one that already
// matches on name+product+active, adding any defense layers it's missing
// (creating layers as needed), or creating a new product/layer(s)/tool as
// needed. Returns the target tool id for each ref, keyed by
// DefenseToolRef.Key().
//
// The match is done using the resolved target product's id, not the
// source's product ref: VECTR generates ref as a random string
// independently on every instance, so a source ref and a target ref for
// what's logically "the same" product (matched by name) will essentially
// never be equal -- comparing raw refs across instances would make this
// match fail almost every time. Resolving the product first (see
// resolveOrCreateDefenseToolProduct) and matching on its target-instance id
// compares two ids that actually live in the same space.
//
// VECTR allows two tools identical in every one of these dimensions (name,
// product, active, layers) to coexist; when that happens vat can't tell
// them apart and deterministically picks the more recently updated one
// (falling back to more recently created) -- a documented limitation, not a
// bug.
func reconcileDefenseTools(ctx context.Context, client graphql.Client, db string, toolsToReconcile map[string]DefenseToolRef) (map[string]string, error) {
	slog.InfoContext(ctx, "Starting defense tool reconciliation", "db", db, "tool_count", len(toolsToReconcile))

	if err := validateDefenseToolRefs(toolsToReconcile); err != nil {
		return nil, err
	}

	existingTools, err := dao.GetAllDefenseTools(ctx, client, db)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not fetch tools: %w", err)
	}
	toolsByKey := make(map[string]dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool, len(existingTools.Bluetools.Nodes))
	for _, t := range existingTools.Bluetools.Nodes {
		key := defenseToolKey(t.Name, t.DefenseToolProduct.Id, t.Active)
		if prev, ok := toolsByKey[key]; ok {
			slog.WarnContext(ctx, "target instance has more than one defense tool with the same name+product+active; picking the more recently updated one", "db", db, "tool-name", t.Name)
			if t.UpdateTime < prev.UpdateTime || (t.UpdateTime == prev.UpdateTime && t.CreateTime <= prev.CreateTime) {
				continue // prev is newer (or equally new) -- keep it
			}
		}
		toolsByKey[key] = t
	}

	existingProductsResp, err := dao.GetAllDefenseToolProducts(ctx, client)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not fetch defense tool products: %w", err)
	}
	productsByRef := make(map[string]dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct, len(existingProductsResp.DefenseToolProducts.Nodes))
	// productsByName is the name fallback used when a source ref doesn't
	// match anything on the target (see resolveOrCreateDefenseToolProduct).
	// VECTR doesn't enforce unique product names, and this index is
	// case-insensitive on top of that, so two products can collapse onto one
	// entry; the last one seen wins, which is an arbitrary choice among
	// equals. Warn when it happens rather than resolving silently: if the
	// existing target tool hangs off the product that lost, its tool key
	// won't match and vat will create a second, near-identical tool next to
	// it. Same known limitation as the duplicate-tool case below, and it
	// converges the same way -- a later restore sees both tools under one key
	// and picks the more recently updated one -- but the duplicate stays in
	// the target until someone cleans it up. Products matched by ref are
	// unaffected: that path is checked first and refs are unique per
	// instance.
	productsByName := make(map[string]dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct, len(existingProductsResp.DefenseToolProducts.Nodes))
	for _, p := range existingProductsResp.DefenseToolProducts.Nodes {
		productsByRef[p.Ref] = p
		nameKey := strings.ToLower(p.Name)
		if prev, ok := productsByName[nameKey]; ok {
			slog.WarnContext(ctx, "target instance has more than one defense tool product with the same name (case-insensitively); the name fallback can only resolve to one of them, which may create a duplicate defense tool",
				"product-name", p.Name, "kept-product-id", p.Id, "kept-product-ref", p.Ref, "ignored-product-id", prev.Id, "ignored-product-ref", prev.Ref)
		}
		productsByName[nameKey] = p
	}

	existingLayersResp, err := dao.GetAllDefensiveLayers(ctx, client, db)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not fetch defensive layers: %w", err)
	}
	layersByName := make(map[string]dao.GetAllDefensiveLayersDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer, len(existingLayersResp.Defensivelayers.Nodes))
	for _, l := range existingLayersResp.Defensivelayers.Nodes {
		layersByName[strings.ToLower(l.Name)] = l
	}

	existingLibraryLayersResp, err := dao.GetAllLibraryDefensiveLayers(ctx, client)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not fetch library defensive layers: %w", err)
	}
	libraryLayersByName := make(map[string]dao.GetAllLibraryDefensiveLayersLibraryDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer, len(existingLibraryLayersResp.LibraryDefensivelayers.Nodes))
	for _, l := range existingLibraryLayersResp.LibraryDefensivelayers.Nodes {
		libraryLayersByName[strings.ToLower(l.Name)] = l
	}

	result := make(map[string]string, len(toolsToReconcile))
	for key, ref := range toolsToReconcile {
		product, err := resolveOrCreateDefenseToolProduct(ctx, client, ref.Product, productsByRef, productsByName, libraryLayersByName)
		if err != nil {
			return nil, err
		}

		targetKey := defenseToolKey(ref.Name, product.Id, ref.Active)
		if existing, ok := toolsByKey[targetKey]; ok {
			slog.DebugContext(ctx, "defense tool matched existing", "tool-name", ref.Name, "product-id", product.Id, "active", ref.Active, "target-tool-id", existing.Id)
			id, err := reconcileExistingDefenseTool(ctx, client, db, existing, ref, layersByName, libraryLayersByName)
			if err != nil {
				return nil, err
			}
			result[key] = id
			continue
		}
		slog.DebugContext(ctx, "defense tool has no existing match, resolving layers to create it", "tool-name", ref.Name, "product-id", product.Id, "active", ref.Active)

		layerIds, err := resolveOrCreateDefenseLayerIds(ctx, client, db, ref.Layers, layersByName, libraryLayersByName)
		if err != nil {
			return nil, err
		}

		r, err := dao.CreateDefenseTool(ctx, client, dao.CreateDefenseToolInput{
			Db: db,
			CreateDefenseToolData: []dao.CreateDefenseToolDataInput{{
				Name:                 ref.Name,
				Description:          ref.Description,
				Active:               ref.Active,
				DefenseToolProductId: product.Id,
				DefenseLayerIds:      layerIds,
			}},
		})
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not create defense tool %q: %w", ref.Name, err)
		}
		if len(r.DefenseTool.Create.DefenseTools) == 0 {
			return nil, fmt.Errorf("creating defense tool %q returned no tool", ref.Name)
		}
		created := r.DefenseTool.Create.DefenseTools[0]
		slog.DebugContext(ctx, "defense tool created", "tool-name", ref.Name, "product-id", product.Id, "target-tool-id", created.Id, "layer-ids", layerIds)
		result[key] = created.Id

		// Fold the new tool into toolsByKey so a later ref in this same run
		// that resolves to the same name+product+active reuses it -- and gets
		// its own layers reconciled onto it -- instead of creating a second,
		// identical tool. Two source refs can land on one target key even
		// though they're distinct keys in toolsToReconcile: they need only
		// differ in product ref and have product names that collapse together
		// under resolveOrCreateDefenseToolProduct's case-insensitive name
		// fallback.
		//
		// CreateTime/UpdateTime aren't in the create payload and stay zero.
		// That's harmless: the duplicate tiebreak above runs only while
		// indexing the pre-existing tools, and this key is by definition
		// unoccupied at this point.
		createdLayers := make([]dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueToolDefensiveLayersDefensiveLayer, 0, len(created.DefensiveLayers))
		for _, l := range created.DefensiveLayers {
			createdLayers = append(createdLayers, dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueToolDefensiveLayersDefensiveLayer{Id: l.Id, Name: l.Name})
		}
		toolsByKey[targetKey] = dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool{
			Id:              created.Id,
			Name:            created.Name,
			Description:     created.Description,
			Active:          created.Active,
			DefensiveLayers: createdLayers,
			DefenseToolProduct: dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueToolDefenseToolProduct{
				Id:  created.DefenseToolProduct.Id,
				Ref: created.DefenseToolProduct.Ref,
			},
		}
	}
	return result, nil
}

// reconcileExistingDefenseTool adds any defense layers ref has that existing
// lacks (creating layers as needed), leaving name/description/product/active
// untouched -- those aren't part of the match criteria (see DefenseToolRef's
// doc comment) so they're never overwritten on an already-matching tool.
func reconcileExistingDefenseTool(ctx context.Context, client graphql.Client, db string, existing dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool, ref DefenseToolRef, layersByName map[string]dao.GetAllDefensiveLayersDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer, libraryLayersByName map[string]dao.GetAllLibraryDefensiveLayersLibraryDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer) (string, error) {
	have := make(map[string]bool, len(existing.DefensiveLayers))
	layerIds := make([]string, 0, len(existing.DefensiveLayers))
	for _, l := range existing.DefensiveLayers {
		have[strings.ToLower(l.Name)] = true
		layerIds = append(layerIds, l.Id)
	}

	var missing []string
	for _, name := range ref.Layers {
		if !have[strings.ToLower(name)] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return existing.Id, nil
	}

	newIds, err := resolveOrCreateDefenseLayerIds(ctx, client, db, missing, layersByName, libraryLayersByName)
	if err != nil {
		return "", err
	}
	layerIds = append(layerIds, newIds...)

	r, err := dao.UpdateDefenseTool(ctx, client, dao.UpdateDefenseToolInput{
		Db: db,
		UpdateDefenseToolData: []dao.UpdateDefenseToolDataInput{{
			Id:                   existing.Id,
			Name:                 existing.Name,
			Description:          existing.Description,
			Active:               existing.Active,
			DefenseToolProductId: existing.DefenseToolProduct.Id,
			DefenseLayerIds:      layerIds,
		}},
	})
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return "", fmt.Errorf("could not update defense tool %q (id %s) with new layers: %w", existing.Name, existing.Id, err)
	}
	if len(r.DefenseTool.Update.DefenseTools) == 0 {
		return "", fmt.Errorf("updating defense tool %q (id %s) returned no tool", existing.Name, existing.Id)
	}
	return r.DefenseTool.Update.DefenseTools[0].Id, nil
}

// resolveOrCreateDefenseLayerIds returns the target instance's id for each
// layer name (case-insensitive), creating any that don't already exist.
// layersByName is updated in place so later calls within the same
// reconcileDefenseTools run reuse anything created here instead of creating
// duplicates.
//
// A db-scoped defense layer can't be created directly: VECTR's create
// mutation for it also tries to create a same-named library-level layer as a
// side effect, and errors outright if one already exists. So creating one
// here instead resolves-or-creates the library-level layer first (via
// libraryLayersByName/resolveOrCreateLibraryDefenseLayerIds, reusing one that
// already exists) and clones that into the db scope, which is the supported
// path regardless of whether the library layer pre-existed.
func resolveOrCreateDefenseLayerIds(ctx context.Context, client graphql.Client, db string, names []string, layersByName map[string]dao.GetAllDefensiveLayersDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer, libraryLayersByName map[string]dao.GetAllLibraryDefensiveLayersLibraryDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer) ([]string, error) {
	ids := make([]string, 0, len(names))
	for _, name := range names {
		key := strings.ToLower(name)
		if l, ok := layersByName[key]; ok {
			slog.DebugContext(ctx, "defense layer matched existing", "layer-name", name, "target-layer-id", l.Id)
			ids = append(ids, l.Id)
			continue
		}

		libraryLayerIds, err := resolveOrCreateLibraryDefenseLayerIds(ctx, client, []DefenseLayer{{Name: name}}, libraryLayersByName)
		if err != nil {
			return nil, fmt.Errorf("could not resolve library defense layer to clone for defense layer %q: %w", name, err)
		}

		r, err := dao.CloneDefenseLayer(ctx, client, dao.CloneLibraryDefenseLayerInput{
			Db:                     db,
			LibraryDefenseLayerIds: libraryLayerIds,
		})
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not create defense layer %q: %w", name, err)
		}
		if len(r.DefenseLayer.Clone.DefenseLayers) == 0 {
			return nil, fmt.Errorf("creating defense layer %q returned no layer", name)
		}
		created := r.DefenseLayer.Clone.DefenseLayers[0]
		slog.DebugContext(ctx, "defense layer created", "layer-name", name, "target-layer-id", created.Id, "library-layer-id", libraryLayerIds[0])
		layersByName[key] = dao.GetAllDefensiveLayersDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer{Id: created.Id, Name: created.Name}
		ids = append(ids, created.Id)
	}
	return ids, nil
}

// resolveOrCreateLibraryDefenseLayerIds returns the target instance's id for
// each library defense layer (case-insensitive match on name), creating any
// that don't already exist. libraryLayersByName is updated in place so later
// calls within the same reconcileDefenseTools run reuse anything created here
// instead of creating duplicates.
//
// Library defense layers (attached to a DefenseToolProduct) are a distinct
// resource from the db-scoped defense layers attached directly to a
// DefenseTool (see resolveOrCreateDefenseLayerIds) -- they live in their own
// id space even when names collide, so they're resolved and cached
// separately rather than sharing layersByName.
func resolveOrCreateLibraryDefenseLayerIds(ctx context.Context, client graphql.Client, layers []DefenseLayer, libraryLayersByName map[string]dao.GetAllLibraryDefensiveLayersLibraryDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer) ([]string, error) {
	ids := make([]string, 0, len(layers))
	for _, layer := range layers {
		key := strings.ToLower(layer.Name)
		if l, ok := libraryLayersByName[key]; ok {
			slog.DebugContext(ctx, "library defense layer matched existing", "layer-name", layer.Name, "target-layer-id", l.Id)
			ids = append(ids, l.Id)
			continue
		}
		r, err := dao.CreateLibraryDefenseLayer(ctx, client, dao.CreateLibraryDefenseLayerInput{
			DefenseLayerData: []dao.CreateLibraryDefenseLayerDataInput{{Name: layer.Name, Description: layer.Description}},
		})
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not create library defense layer %q: %w", layer.Name, err)
		}
		if len(r.DefenseLayer.CreateLibrary.DefenseLayers) == 0 {
			return nil, fmt.Errorf("creating library defense layer %q returned no layer", layer.Name)
		}
		created := r.DefenseLayer.CreateLibrary.DefenseLayers[0]
		slog.DebugContext(ctx, "library defense layer created", "layer-name", layer.Name, "target-layer-id", created.Id)
		libraryLayersByName[key] = dao.GetAllLibraryDefensiveLayersLibraryDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer{Id: created.Id, Name: created.Name}
		ids = append(ids, created.Id)
	}
	return ids, nil
}

// resolveOrCreateDefenseToolProduct finds ref's matching product, creating
// it (and resolving its vendor by name and library defense layers, if any)
// if absent. productsByRef and productsByName are updated in place with
// anything created.
//
// Matching tries ref.Ref first, then falls back to a case-insensitive name
// match: CreateDefenseToolProductDataInput has no ref field, VECTR derives
// ref itself on create, so a newly created product's ref may not equal the
// source's ref -- a later restore of the same source data would never match
// it by ref again. Name is the durable fallback for that case. When the name
// fallback matches, productsByRef is also backfilled under ref.Ref so later
// refs in this same run pointing at this product hit the fast path.
//
// If a product already exists (by either match), it's taken as-is -- its
// layers are never diffed or backfilled here (unlike
// reconcileExistingDefenseTool's handling of a tool's own db-scoped layers).
func resolveOrCreateDefenseToolProduct(ctx context.Context, client graphql.Client, ref DefenseToolProductRef, productsByRef map[string]dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct, productsByName map[string]dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct, libraryLayersByName map[string]dao.GetAllLibraryDefensiveLayersLibraryDefensivelayersDefensiveLayerConnectionNodesDefensiveLayer) (dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct, error) {
	if p, ok := productsByRef[ref.Ref]; ok {
		slog.DebugContext(ctx, "defense tool product matched existing by ref", "product-name", ref.Name, "product-ref", ref.Ref, "target-product-id", p.Id)
		return p, nil
	}
	if p, ok := productsByName[strings.ToLower(ref.Name)]; ok {
		slog.DebugContext(ctx, "defense tool product matched existing by name fallback (ref mismatch)", "product-name", ref.Name, "source-ref", ref.Ref, "target-product-id", p.Id, "target-product-ref", p.Ref)
		productsByRef[ref.Ref] = p
		return p, nil
	}

	var vendorId *string
	if ref.VendorName != "" {
		vr, err := dao.FindVendor(ctx, client, ref.VendorName)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct{}, fmt.Errorf("could not look up vendor %q: %w", ref.VendorName, err)
		}
		if len(vr.LibraryVendors.Nodes) > 0 {
			id := vr.LibraryVendors.Nodes[0].Id
			vendorId = &id
		} else {
			slog.WarnContext(ctx, "vendor not found in target instance, creating defense tool product without a vendor", "vendor-name", ref.VendorName, "product-ref", ref.Ref)
		}
	}

	layerIds, err := resolveOrCreateLibraryDefenseLayerIds(ctx, client, ref.Layers, libraryLayersByName)
	if err != nil {
		return dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct{}, fmt.Errorf("could not resolve library defense layers for product %q (source ref %q): %w", ref.Name, ref.Ref, err)
	}

	r, err := dao.CreateDefenseToolProduct(ctx, client, dao.CreateDefenseToolProductInput{
		DefenseToolProducts: []dao.CreateDefenseToolProductDataInput{{
			Name:            ref.Name,
			VendorId:        vendorId,
			DefenseLayerIds: layerIds,
		}},
	})
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct{}, fmt.Errorf("could not create defense tool product %q (source ref %q): %w", ref.Name, ref.Ref, err)
	}
	if len(r.DefenseToolProduct.Create.DefenseToolProducts) == 0 {
		return dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct{}, fmt.Errorf("creating defense tool product %q (source ref %q) returned no product", ref.Name, ref.Ref)
	}
	created := r.DefenseToolProduct.Create.DefenseToolProducts[0]
	product := dao.GetAllDefenseToolProductsDefenseToolProductsDefenseToolProductsConnectionNodesDefenseToolProduct{
		Id:   created.Id,
		Name: created.Name,
		Ref:  created.Ref,
	}
	slog.DebugContext(ctx, "defense tool product created", "product-name", ref.Name, "source-ref", ref.Ref, "target-product-id", product.Id, "target-product-ref", product.Ref, "vendor-id", vendorId, "layer-ids", layerIds)
	productsByRef[product.Ref] = product
	productsByName[strings.ToLower(product.Name)] = product
	return product, nil
}

// restoreCampaigns creates campaigns and their associated test cases within a
// specified assessment. It handles the mapping of organizations, tools, and
// metadata from the serialized data to the target VECTR instance.
//
// Parameters:
//   - ctx: The context for managing request lifetimes and cancellations.
//   - client: The GraphQL client for interacting with the VECTR instance.
//   - db: The database name in the VECTR instance.
//   - assessmentId: The ID of the assessment in the target instance where
//     campaigns will be created.
//   - assessmentName: The name of the assessment, used for logging and error
//     reporting.
//   - campaignsToRestore: A slice of campaign data objects to be restored.
//   - orgMap: A map of organization names to their corresponding objects in
//     the target instance, used for resolving organization IDs.
//   - toolIdByKey: A map of DefenseToolRef.Key() to the corresponding tool's
//     id in the target instance (see reconcileDefenseTools), used for
//     resolving tool IDs.
//   - idToolsMap: A map of serialized tool IDs to their DefenseToolRef,
//     used to map outcomes from the serialized data to the target instance.
//
// Returns:
//   - error: Returns nil on success, or an error if campaign or test case creation fails.
//
// Error Handling:
//   - Returns an error if campaign creation fails via the GraphQL API.
//   - Returns an error if a test case outcome status is not found in the mapping.
//   - Returns an error if test case creation (with or without templates) fails.
func restoreCampaigns(
	ctx context.Context,
	client graphql.Client,
	db string,
	assessmentId string,
	assessmentName string,
	campaignsToRestore []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign,
	orgMap map[string]dao.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization,
	toolIdByKey map[string]string,
	idToolsMap map[string]DefenseToolRef,
	optionalParams *RestoreOptionalParams,
) error {
	// Step 5: Create the campaigns
	campaigns := dao.CreateCampaignInput{
		Db:           db,
		AssessmentId: assessmentId,
		CampaignData: []dao.CreateCampaignDataInput{},
	}
	for _, c := range campaignsToRestore {
		campaign := dao.CreateCampaignDataInput{
			Name:        c.Name,
			Description: c.Description,
		}
		for _, o := range c.Organizations {
			campaign.OrganizationIds = append(campaign.OrganizationIds, orgMap[o.Name].Id)
		}
		for _, md := range c.Metadata {
			campaign.Metadata = append(campaign.Metadata, dao.MetadataKeyValuePairInput(md))
		}
		campaigns.CampaignData = append(campaigns.CampaignData, campaign)
	}
	slog.DebugContext(ctx, "Creating campaigns",
		"count", len(campaigns.CampaignData),
		"assessment_name", assessmentName)
	r, err := dao.CreateCampaigns(ctx, client, campaigns)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not create campaigns for %s, suggest deleting the assessment: %w", assessmentName, err)
	}
	// Note that this creates a bug where if two campaigns are the same name, it will not work.
	// To be fixed if you'll need to insert each campaign individually so you can map them
	// For now this is fine
	campaign_map := make(map[string]string)
	for _, cdata := range r.Campaign.Create.Campaigns {
		campaign_map[cdata.Name] = cdata.Id
	}

	slog.InfoContext(ctx, "Campaigns created",
		"count", len(campaigns.CampaignData),
		"assessment_name", assessmentName)

	// Step 6: Create the test cases but need to do a calculation if the highest outcome from the tool doesn't match the test case, set override
	testCaseCount := 0
	for _, c := range campaignsToRestore {
		// there could be a mix of test case types in a campaign, so add both types in
		tc_with_library := NewGroupedCreateTestCaseWithLibraryIdInput(db, campaign_map[c.Name])

		tc_no_template := dao.CreateTestCaseWithoutTemplateInput{
			Db:                         db,
			CampaignId:                 campaign_map[c.Name],
			TestCaseData:               []dao.CreateTestCaseDataInput{},
			SuppressAutoTimelineEvents: true,
		}

		timelineEntriesCount := 0
		// have to do this here (maybe make this an object in the future)
		// but basically, I need to check if the outcome is in the map
		// if it is not, throw an error
		for _, serialized_tc := range c.TestCases {
			status, ok := outcomeStatusMap[serialized_tc.Status]
			if !ok {
				slog.WarnContext(ctx, "could not find outcome for this test case, passing it through as-is (forwards compat)", "outcome", serialized_tc.Status, "test-case", serialized_tc.Name, "campaign", c.Name, "campaign-id", c.Id, "test-case-id", serialized_tc.Id)
				status = dao.TestCaseStatus(serialized_tc.Status)
			}
			timelineEntriesCount += len(serialized_tc.TimelineEvents)
			// OrgMap is a resource vat itself manages and requires -- every test
			// case must carry an organization. Confirmed against a live instance
			// that VECTR rejects a create with a blank organization, but we don't
			// defer to that: a missing org here means our own data is
			// inconsistent, so fail fast with a clear error instead of letting
			// VECTR reject it downstream with a less useful message.
			if len(serialized_tc.Organizations) == 0 {
				slog.ErrorContext(ctx, "test case has no organization set", "test-case", serialized_tc.Name, "campaign", c.Name, "campaign-id", c.Id, "test-case-id", serialized_tc.Id)
				return fmt.Errorf("test case %s (campaign %s) has no organization set", serialized_tc.Id, c.Name)
			}
			organization := serialized_tc.Organizations[0].Name
			testCaseData := dao.CreateTestCaseDataInput{
				// Correlation token echoed back by VECTR on the created item; the
				// source test case id is already unique within this request, so
				// there's no need to mint a fresh one.
				ClientId:         serialized_tc.Id,
				Name:             serialized_tc.Name,
				Description:      serialized_tc.Description,
				Phase:            serialized_tc.Phase.Name,
				Technique:        serialized_tc.MitreId,
				Organization:     organization,
				Status:           status,
				DetectionSteps:   serialized_tc.DetectionGuidance,
				PreventionSteps:  serialized_tc.PreventionGuidance,
				OutcomePath:      serialized_tc.Outcome.Path,
				OutcomeNotes:     serialized_tc.OutcomeNotes,
				References:       serialized_tc.References,
				OperatorGuidance: serialized_tc.OperatorGuidance,
				DataVer:          serialized_tc.DataVer,
				OverrideOutcome:  serialized_tc.OverrideOutcome,
				UserContext:      serialized_tc.UserContext,
				//AttackSuccess:    serialized_tc.AttackSuccess, // handled below
				// these are no longer required, they are handled by timeline events
				//AttackStart:      serialized_tc.AttackStart.CreateTime,
				//AttackStop:       serialized_tc.AttackStop.CreateTime,
				//DetectionTime:    serialized_tc.DetectionTime.CreateTime,
				//Tags:                  []string{}, //to be handled below
				//Targets:               []string{}, // to be handled below
				//Sources:               []string{},
				//Defenses:              []string{},
				//DetectingDefenseTools: []DefenseToolInput{},          // handle below
				//RedTeamMetadata:       []MetadataKeyValuePairInput{}, //handle below
				//BlueTeamMetadata:      []MetadataKeyValuePairInput{}, // handle below
				//AttackAutomation:      AttackAutomationInput{},       //handle below
				//RedTools:              []RedToolInput{},
				//DefenseToolOutcomes:   []DefenseToolOutcomeInput{},   // handle below
			}

			if len(strings.TrimSpace(string(serialized_tc.AttackSuccess))) == 0 {
				testCaseData.AttackSuccess = nil
			} else {
				testCaseData.AttackSuccess = &serialized_tc.AttackSuccess
			}
			// Need to check if this logic has issues still
			//if testCaseData.AttackStart == 0 {
			//	slog.WarnContext(ctx, "Attack Start is set to 0, reset to the AttackStop time", "attack-stop-time", testCaseData.AttackStop, "campaign-name", c.Name, "test-case-name", serialized_tc.Name)
			//	testCaseData.AttackStart = testCaseData.AttackStop
			//}
			for _, tag := range serialized_tc.Tags {
				testCaseData.Tags = append(testCaseData.Tags, tag.Name)
			}
			for _, target := range serialized_tc.Targets {
				testCaseData.Targets = append(testCaseData.Targets, target.Name)
			}
			for _, source := range serialized_tc.Sources {
				testCaseData.Sources = append(testCaseData.Sources, source.Name)
			}
			for _, defense := range serialized_tc.DefensiveLayers {
				testCaseData.Defenses = append(testCaseData.Defenses, defense.Name)
			}
			for _, detectingdefensetool := range serialized_tc.BlueTools {
				testCaseData.DetectingDefenseTools = append(testCaseData.DetectingDefenseTools, dao.DefenseToolInput{
					Name: detectingdefensetool.Name,
				})
			}
			for _, md := range serialized_tc.Metadata {
				testCaseData.RedTeamMetadata = append(testCaseData.RedTeamMetadata, dao.MetadataKeyValuePairInput(md))
			}
			if serialized_tc.AutomationCmd != "" {
				var errors []error
				testCaseData.AttackAutomation, errors = buildAttackAutomationInput(&serialized_tc)
				if len(errors) > 0 {
					for _, err := range errors {
						slog.WarnContext(ctx, "parsing discrepencies found, they were recovered but review if needed",
							"test-case-id", serialized_tc.Id,
							"test-case-name", serialized_tc.Name,
							"campaign", c.Name,
							"campaign-id", c.Id,
							"assessment-name", assessmentName,
							"db", db,
							"err", err,
						)

					}
				}
			}
			for _, redtool := range serialized_tc.RedTools {
				testCaseData.RedTools = append(testCaseData.RedTools, dao.RedToolInput{
					Name: redtool.Name,
				})
			}

			for _, result := range serialized_tc.DefenseToolOutcomes {
				testCaseData.DefenseToolOutcomes = append(testCaseData.DefenseToolOutcomes, dao.DefenseToolOutcomeInput{
					// take the stringified integer from the serialized data, look up the source tool's ref from the original data set,
					//		and then look up the reconciled id in the new instance
					DefenseToolId: toolIdByKey[idToolsMap[strconv.Itoa(result.DefenseToolId)].Key()],
					OutcomeId:     result.OutcomeId,
				})
			}
			// if there is no library test case id, then add with no template
			if optionalParams.ForceEnvOnly || (serialized_tc.LibraryTestCaseId == "" || serialized_tc.LibraryTestCaseId == "null") {
				tc_no_template.TestCaseData = append(tc_no_template.TestCaseData, testCaseData)
			} else {
				// otherwise, create with template
				tcd := dao.CreateTestCaseDataWithLibraryIdInput{
					LibraryTestCaseId:    serialized_tc.LibraryTestCaseId,
					CreateNewIfNotExists: false,
					TestCaseData:         testCaseData,
				}
				tc_with_library.Add(tcd)
			}
		}
		slog.DebugContext(ctx, "Creating test cases",
			"campaign_name", c.Name,
			"test_case_count", tc_with_library.Len(),
			"test-case-count-no-template", len(tc_no_template.TestCaseData),
			"assessment_name", assessmentName)
		// source test case ID (clientId) -> new test case ID, for this campaign only.
		testCaseIdMap := make(map[string]string, tc_with_library.Len()+len(tc_no_template.TestCaseData))
		if tc_with_library.Len() > 0 {
			for _, batch := range tc_with_library.GenerateInsertsData() {
				r, err := dao.CreateTestCasesByLibraryId(ctx, client, batch)
				if err != nil {
					if gqlObject, ok := gqlErrParse(err); ok {
						slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
					}
					return fmt.Errorf("could not write test cases for %s, campaign: %s; check vectr version: %w", assessmentName, c.Name, err)
				}
				for _, item := range r.TestCase.CreateWithTemplateMatchByLibraryId.TestCaseCreateItems {
					testCaseIdMap[item.ClientId] = item.TestCase.Id
				}
				testCaseCount += len(batch.CreateTestCaseInputs)
			}
		}
		if len(tc_no_template.TestCaseData) > 0 {
			r, err := dao.CreateTestCasesNoTemplate(ctx, client, tc_no_template)
			if err != nil {
				if gqlObject, ok := gqlErrParse(err); ok {
					slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
				}
				return fmt.Errorf("could not write test cases for %s: %w", assessmentName, err)
			}
			for _, item := range r.TestCase.CreateWithoutTemplate.TestCaseCreateItems {
				testCaseIdMap[item.ClientId] = item.TestCase.Id
			}
			testCaseCount += len(tc_no_template.TestCaseData)
		}
		// Here's where we add the timelines
		timelineEventInsert := &dao.CreateTimelineEventsInput{
			Db:     db,
			Events: make([]dao.TimelineEventInput, 0, timelineEntriesCount),
		}

		for _, stc := range c.TestCases {
			if _, ok := testCaseIdMap[stc.Id]; !ok {
				continue
			}
			for _, te := range stc.TimelineEvents {
				teToInsert := &dao.TimelineEventInput{
					ClientId:    uuid.NewString(),
					TestCaseId:  testCaseIdMap[stc.Id],
					Team:        te.Team,
					Description: te.ManualDescription, // I _think_ I can just write this. If there is nothing there, then this becomes an omit empty
					Type:        te.Type,
					CreateTime:  time.UnixMilli(int64(te.CreateTime)),
					Designation: te.Designation,
					//ToolOutcomeChange: dao.ToolOutcomeChangeEventInput{
					//	OutcomeId: te.ToolOutcomeChange.OutcomeId,
					//	ToolId:    toolIdByKey[idToolsMap[strconv.Itoa(te.ToolOutcomeChange.DefenseToolId)].Key()],
					//},
				}

				if strings.EqualFold(te.Type, "Manual") {
					teToInsert.ManualEvent = &dao.ManualTimelineEventInput{
						Placeholder: true,
					}
				} else if strings.EqualFold(te.Type, "FieldChange") {
					switch {
					case strings.EqualFold(te.FieldName, "status"):
						status, ok := outcomeStatusMap[te.FieldAction]
						if !ok {
							slog.WarnContext(ctx, "unrecognized status in status field-change event, passing it through as-is (forwards compat)",
								"assessment-name", assessmentName,
								"campaign_name", c.Name,
								"source-test-case-id", stc.Id,
								"source-test-case-name", stc.Name,
								"timeline-event-id", te.Id,
								"status", te.FieldAction)
							status = dao.TestCaseStatus(te.FieldAction)
						}
						teToInsert.StatusChange = &dao.StatusChangeEventInput{
							Status: status,
						}
					case strings.EqualFold(te.FieldName, "outcomeId"):
						teToInsert.OutcomeChange = &dao.OutcomeChangeEventInput{
							OutcomeId: te.FieldAction,
						}
						if te.ToolOutcomeChange != nil {
							teToInsert.ToolOutcomeChange = &dao.ToolOutcomeChangeEventInput{
								OutcomeId: te.ToolOutcomeChange.OutcomeId,
								ToolId:    toolIdByKey[idToolsMap[strconv.Itoa(te.ToolOutcomeChange.DefenseToolId)].Key()],
							}
						}
					case strings.EqualFold(te.FieldName, "toolOutcome"):
						if te.ToolOutcomeChange == nil {
							slog.ErrorContext(ctx, "toolOutcome field change event is missing its ToolOutcomeChange payload",
								"assessment-name", assessmentName,
								"campaign_name", c.Name,
								"source-test-case-id", stc.Id,
								"source-test-case-name", stc.Name,
								"timeline-event-id", te.Id)
							return fmt.Errorf("timeline event for test case %s has field-name toolOutcome but no ToolOutcomeChange data", stc.Id)
						}
						teToInsert.ToolOutcomeChange = &dao.ToolOutcomeChangeEventInput{
							OutcomeId: te.ToolOutcomeChange.OutcomeId,
							ToolId:    toolIdByKey[idToolsMap[strconv.Itoa(te.ToolOutcomeChange.DefenseToolId)].Key()],
						}
					default:
						slog.WarnContext(ctx, "unrecognized field change field name, skipping (forwards compat)", "assessment-name", assessmentName, "field-name", te.FieldName)
					}
				} else {
					slog.WarnContext(ctx, "unrecognized timeline event type, skipping (forwards compat)", "assessment-name", assessmentName, "event-type", te.Type)
				}
				slog.DebugContext(ctx, "Prepared timeline event",
					"assessment-name", assessmentName,
					"campaign_name", c.Name,
					"source-test-case-id", stc.Id,
					"test-case-id", teToInsert.TestCaseId,
					"event-type", te.Type,
					"event", teToInsert)
				timelineEventInsert.Events = append(timelineEventInsert.Events, *teToInsert)
			}
		}
		respTimelineResponse, err := dao.CreateTimelineEvents(ctx, client, *timelineEventInsert)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return fmt.Errorf("could not write timeline events for %s, campaign: %s; check vectr version: %w", assessmentName, c.Name, err)
		}
		// this is a way to check if errors happened as well
		if respTimelineResponse.TimelineEvent.Create.Summary.Failed > 0 {
			for _, errmsg := range respTimelineResponse.TimelineEvent.Create.Items {
				if len(errmsg.Errors) > 0 {
					for _, te := range timelineEventInsert.Events {
						if strings.EqualFold(errmsg.ClientId, te.ClientId) {
							slog.ErrorContext(ctx, "failed to create timeline event",
								"assessment-name", assessmentName,
								"campaign_name", c.Name,
								"client-id", te.ClientId,
								"test-case-id", te.TestCaseId,
								"event", te,
								"errors", errmsg.Errors)
							break
						}

					}
				}
			}
			return fmt.Errorf("could not write timeline events for %s, campaign: %s; %d", assessmentName, c.Name, respTimelineResponse.TimelineEvent.Create.Summary.Failed)
		}
	}
	slog.InfoContext(ctx, "Test cases created", "assessment-name", assessmentName, "test-case-count", testCaseCount)

	return nil
}

// validateLibraryTestCases checks if a list of library test case IDs exist in the target VECTR instance.
// It performs a query and specifically handles the GraphQL error case where some IDs are not found,
// returning a detailed error message.
func validateLibraryTestCases(ctx context.Context, client graphql.Client, libraryTestCaseIDs []string, templateAssessmentName string) error {
	if len(libraryTestCaseIDs) == 0 {
		return nil
	}
	// first time, we never really need to check the response, if the missing ids remain none,
	// we don't need to do anything
	_, err := dao.GetLibraryTestCases(ctx, client, libraryTestCaseIDs)
	if err == nil {
		return nil
	}

	var missing_ids []string
	gqlerrlist, ok := err.(gqlerror.List)
	if !ok {
		return fmt.Errorf("could not fetch library test cases for %s: %w", templateAssessmentName, err)
	}

	// the error type we expect only has one entry for this path
	if !(len(gqlerrlist) == 1 && gqlerrlist[0].Path.String() == "libraryTestcasesByIds") {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not fetch library test cases for %s: %w", templateAssessmentName, err)
	}
	// there should be an `ids` field in the extensions object
	rawids, ok := gqlerrlist[0].Extensions["ids"]
	if !ok {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not fetch library test cases for %s: %w", templateAssessmentName, err)
	}
	// the `ids` filed should only have one entry
	ids, ok := rawids.([]any)
	if !(ok && len(ids) == 1) {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not fetch library test cases for %s: %w", templateAssessmentName, err)
	}

	id := ids[0].(string)
	if !strings.HasPrefix(id, "The following IDs were not valid") {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not fetch library test cases for %s: %w", templateAssessmentName, err)
	}
	// this is a case where we got an error back for an otherwise valid query, one or more of the ids are not valid
	mids, err := ParseLibraryTestcasesByIdsError(id)
	if err != nil {
		return fmt.Errorf("could not fetch library test cases for %s: %w", templateAssessmentName, err)
	}
	missing_ids = append(missing_ids, mids...)

	if len(missing_ids) > 0 {
		slog.ErrorContext(ctx, "could not find all the ids in the instance", "missing-ids", missing_ids)
		return fmt.Errorf("could not find all the ids in the instance, override templates to insert, missing id count: %d", len(missing_ids))
	}

	return nil
}

// RestoreAssessment restores an assessment to a VECTR instance by deserializing
// and importing serialized assessment data. It ensures that all required
// organizations, tools, and templates exist in the target instance before
// creating the assessment, campaigns, and test cases.
//
// Parameters:
//   - ctx: The context for managing request lifetimes and cancellations.
//   - client: The GraphQL client for interacting with the VECTR instance.
//   - db: The database name in the VECTR instance.
//   - ad: The serialized assessment data to restore, including organizations,
//     tools, campaigns, and test cases.
//   - optionalParams: Optional parameters to customize the restore process,
//     such as overriding the assessment name or skipping template validation.
//
// Returns:
//   - error: Returns an error if any step of the restore process fails. The error
//     message provides details about the failure.
//
// Workflow:
// 1. **Validate Organizations**:
//   - Checks if all organizations in the serialized data exist in the target
//     VECTR instance.
//   - If any organization is missing, the function returns an error listing
//     the missing organizations.
//
// 2. **Validate Tools**:
//   - Verifies that all tools in the serialized data exist in the target
//     instance.
//   - If any tools are missing, the function returns an error listing the
//     missing tools along with their names and product information.
//
// 3. **Handle Template Assessment**:
//   - If `OverrideAssessmentTemplate` is set, it creates template test cases
//     directly from the serialized data.
//   - Otherwise, it validates that the required template assessment or
//     individual library test cases exist in the target instance.
//
// 4. **Override Assessment Name**:
//   - If `optionalParams.AssessmentName` is provided, it overrides the name
//     of the assessment in the serialized data.
//
// 5. **Create Assessment**:
//   - Creates the assessment in the target instance using the serialized data.
//   - Includes metadata and organization mappings.
//
// 6. **Restore Campaigns**:
//   - Calls `restoreCampaigns` to populate the assessment with campaigns
//     and test cases.
//   - If `DeleteOnFailure` is true, it rolls back the assessment creation
//     if campaign restoration fails.
//
// Error Handling:
// The function returns detailed errors for the following scenarios:
//   - Missing organizations (`ErrOrgNotFound`).
//   - Missing library assessments (`ErrMissingLibraryAssessment`).
//   - A local assessment already exists (`ErrAssessmentAlreadyExists`).
//   - Invalid or blank assessment name overrides (`ErrInvalidAssessmentName`).
//   - GraphQL API errors during organization, tool, template, assessment,
//     campaign, or test case creation.
func RestoreAssessment(ctx context.Context, client graphql.Client, db string, ad *AssessmentData, optionalParams *RestoreOptionalParams) error {
	slog.InfoContext(ctx, "Starting RestoreAssessment", "db", db, "assessment_name", ad.Assessment.Name)

	// restoreInfo is an artifact of this single restore call — it's never
	// stored on AssessmentData (see its doc comment), just used here for
	// the version-mismatch warning and folded into the target VECTR
	// instance's own metadata below.
	restoreInfo := NewVatOpMetadata(ctx)

	if ad.Manifest.VectrVersion != "" && ad.Manifest.VectrVersion != restoreInfo.VectrVersion {
		slog.WarnContext(ctx, "Save data does not match version you are loading into. The restore may not work correctly", "save-vectr-version", ad.Manifest.VectrVersion, "live-vectr-version", restoreInfo.VectrVersion)
	}

	org_map, err := validateRestorePrerequisites(ctx, client, db, ad.OrgMap)
	if err != nil {
		return err
	}

	toolIdByKey, err := reconcileDefenseTools(ctx, client, db, ad.ToolsMap)
	if err != nil {
		return err
	}

	if optionalParams.AssessmentName != "" {
		slog.DebugContext(ctx, "overiding assessment name", "old-assessment-name", ad.Assessment.Name, "new-assessment-name", optionalParams.AssessmentName)
		ad.Assessment.Name = optionalParams.AssessmentName
	}

	if optionalParams.ResetGlobalId {
		newGlobalId, err := uuid.NewRandom()
		if err != nil {
			return fmt.Errorf("when re-writing global id (--reset-id) could not generate uuid: %w", err)
		}
		slog.DebugContext(ctx, "resetting assessment global id", "assessment-name", ad.Assessment.Name, "old-global-id", ad.Assessment.GlobalId, "new-global-id", newGlobalId.String())
		ad.Assessment.GlobalId = newGlobalId.String()
	}

	lookup_assessments, err := dao.FindExistingAssessment(ctx, client, db, ad.Assessment.Name)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not fetch data about assessment %s, error: %w", ad.Assessment.Name, err)
	}
	if len(lookup_assessments.Assessments.Nodes) > 0 {
		return fmt.Errorf("could not add %s into %s: %w", ad.Assessment.Name, db, ErrAssessmentAlreadyExists)
	}

	// Step 3: Check if there is a template name in the seralized data, if so check in the instance (error if not)
	// If the user wants to ignore error, go ahead and import template test cases
	// If no template name, then go ahead and add template test cases in
	if optionalParams.ForceEnvOnly {
		slog.WarnContext(ctx, "--force-env-only set, skipping template/library test case validation", "assessment-name", ad.Assessment.Name)
	} else {
		if optionalParams.OverrideAssessmentTemplate {
			slog.DebugContext(ctx, "adding template test cases directly")
			input := dao.CreateTestCaseTemplateInput{
				Overwrite:            true,
				TestCaseTemplateData: []dao.CreateTestCaseTemplateDataInput{},
			}

			if len(ad.LibraryTestCases) > 0 {
				for _, template_test_case := range ad.LibraryTestCases {
					slog.DebugContext(ctx, "library test case", "name", template_test_case.Name, "template_id", template_test_case.LibraryTestCaseId)
					tctd, errors, err := createTemplateData(template_test_case)
					if err != nil {
						slog.ErrorContext(ctx, "could not build template test case data",
							"test-case-id", template_test_case.Id,
							"test-case-library-id", template_test_case.LibraryTestCaseId,
							"test-case-name", template_test_case.Name,
							"assessment-name", ad.Assessment.Name,
							"db", db,
							"err", err,
						)
						return err
					}
					if len(errors) > 0 {
						for _, err := range errors {
							slog.WarnContext(ctx, "parsing discrepencies found, they were recovered but review if needed",
								"test-case-id", template_test_case.Id,
								"test-case-library-id", template_test_case.LibraryTestCaseId,
								"test-case-name", template_test_case.Name,
								"assessment-name", ad.Assessment.Name,
								"db", db,
								"err", err,
							)

						}
					}
					input.TestCaseTemplateData = append(input.TestCaseTemplateData, tctd)
				}

				_, err := dao.CreateTemplateTestCases(ctx, client, input)
				if err != nil {
					if gqlObject, ok := gqlErrParse(err); ok {
						slog.ErrorContext(ctx, "full gql error", "error", gqlObject)
					}

					return fmt.Errorf("could not write template test cases: %w", err)
				}
				slog.InfoContext(ctx, "inserted all library test cases", "total", len(input.TestCaseTemplateData))
			} else {
				slog.InfoContext(ctx, "No library test cases found", "assessment-name", ad.Assessment.Name)
			}

		} else {
			if ad.TemplateAssessment != "" {
				slog.DebugContext(ctx, "Validating template assessment in instance",
					"template_assessment", ad.TemplateAssessment,
					"override_template", optionalParams.OverrideAssessmentTemplate)
				prefix := ""
				for _, md := range ad.Assessment.Metadata {
					if md.Key == "prefix" {
						prefix = md.Value + " - "
						break
					}
				}
				t, err := dao.FindLibraryAssessment(ctx, client, prefix+ad.TemplateAssessment)
				if err != nil {
					if gqlObject, ok := gqlErrParse(err); ok {
						slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
					}
					return fmt.Errorf("could not fetch library assessment for %s: %w", ad.TemplateAssessment, err)
				}
				// if the defined library assessment does not exist, check to see if we have all library test cases
				if len(t.LibraryAssessments.Nodes) == 0 {
					slog.WarnContext(ctx, "Could not find library assessment, but checking all the test cases.", "template_assessment", ad.TemplateAssessment)
				}
			}
			// now let's check the actual data
			ids := slices.Collect(maps.Keys(ad.LibraryTestCases))
			if err := validateLibraryTestCases(ctx, client, ids, ad.TemplateAssessment); err != nil {
				return err
			}

		}
	}
	// Step 4: Create the assessment
	slog.InfoContext(ctx, "Creating assessment",
		"assessment_name", ad.Assessment.Name)
	assessment := &dao.CreateAssessmentInput{
		Db: db,
		AssessmentData: []dao.CreateAssessmentDataInput{
			{
				Name:        ad.Assessment.Name,
				Description: ad.Assessment.Description,
				KillChainId: ad.Assessment.KillChain.Id,
				DataVer:     ad.Assessment.DefaultTcDataVer,
				GlobalId:    ad.Assessment.GlobalId,
				//OrganizationIds: []string{}, //handle below
				//Metadata: []MetadataKeyValuePairInput{}, // handle below
			},
		},
	}

	for _, o := range ad.Assessment.Organizations {
		assessment.AssessmentData[0].OrganizationIds = append(assessment.AssessmentData[0].OrganizationIds, org_map[o.Name].Id)
	}
	ad.Assessment.Metadata = loadVatMetadata(ad.Assessment.Metadata, ad.Manifest, restoreInfo)
	for _, md := range ad.Assessment.Metadata {
		assessment.AssessmentData[0].Metadata = append(assessment.AssessmentData[0].Metadata, dao.MetadataKeyValuePairInput(md))
	}

	a, err := dao.CreateAssessment(ctx, client, *assessment)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		if isDuplicateGlobalIdError(err) {
			return fmt.Errorf("could not create assessment container: %s, global-id: %s: %w", assessment.AssessmentData[0].Name, assessment.AssessmentData[0].GlobalId, ErrDuplicateGlobalId)
		}
		return fmt.Errorf("could not create assessment container: %s: %w", assessment.AssessmentData[0].Name, err)
	}
	//a.Assessment.Create.Assessments[0].Id

	err = restoreCampaigns(ctx, client, db, a.Assessment.Create.Assessments[0].Id, ad.Assessment.Name, ad.Assessment.Campaigns, org_map, toolIdByKey, ad.IdToolsMap, optionalParams)
	if err != nil {
		if optionalParams.DeleteOnFailure {
			slog.ErrorContext(ctx, "deleting assessment since a failure occured", "assessment-name", ad.Assessment.Name, "db", db)
			ids, delErr := dao.DeleteAssessment(ctx, client, db, []string{a.Assessment.Create.Assessments[0].Id})
			if delErr != nil {
				// uh oh, things are very bad here.
				slog.ErrorContext(ctx, "could not delete assessment", "error", delErr, "assessment-name", ad.Assessment.Name, "db", db)
			} else { // use an else to fall back to the same return below since we want to output both problems
				if len(ids.Assessment.Delete.DeletedIds) == 1 && strings.EqualFold(ids.Assessment.Delete.DeletedIds[0], a.Assessment.Create.Assessments[0].Id) {
					slog.InfoContext(ctx, "Assessment cleaned up successfully...", "assessment-name", ad.Assessment.Name, "db", db)
				} else {
					slog.ErrorContext(ctx, "delete mismatch, the wrong item was deleted (not sure how this happened)", "expected-id", a.Assessment.Create.Assessments[0].Id, "deleted-id(s)", ids.Assessment.Delete.DeletedIds)
				}
			}
		}
		return fmt.Errorf("could not create campaigns and test cases for assessment %s: %w", ad.Assessment.Name, err)
	}

	slog.InfoContext(ctx, "Assessment restored successfully", "assessment-name", ad.Assessment.Name)
	return nil

}

// RestoreCampaign restores a specific campaign from serialized assessment data
// into an existing assessment in the target VECTR instance. It validates
// prerequisites such as organizations and tools before proceeding with the
// restore.
//
// Parameters:
//   - ctx: The context for managing request lifetimes and cancellations.
//   - client: The GraphQL client for interacting with the VECTR instance.
//   - db: The database name in the VECTR instance.
//   - ad: The serialized assessment data containing the campaign to restore.
//   - sourceCampaignName: The name of the campaign within the assessment data
//     to be restored.
//   - targetAssessmentName: The name of the existing assessment in the target
//     instance where the campaign should be added.
//
// Returns:
//   - error: Returns nil on success, or an error if the campaign cannot be
//     found, prerequisites are missing, or the restore process fails.
//
// Error Handling:
//   - Returns `ErrCampaignNotFound` if the source campaign is not in the data.
//   - Returns an error if the target assessment is not found in the database.
//   - Returns an error if library test cases, organizations, or tools are
//     missing in the target instance.
//   - Returns any error propagated from `restoreCampaigns`.
func RestoreCampaign(ctx context.Context, client graphql.Client, db string, ad *AssessmentData, sourceCampaignName, targetAssessmentName string, optionalParams *RestoreOptionalParams) error {
	slog.InfoContext(ctx, "Starting RestoreCampaign", "db", db, "source_campaign", sourceCampaignName, "target_assessment", targetAssessmentName)

	var campaignToRestore dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign
	found := false
	for _, c := range ad.Assessment.Campaigns {
		if c.Name == sourceCampaignName {
			campaignToRestore = c
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("in assessment data for '%s': %w: %s", ad.Assessment.Name, ErrCampaignNotFound, sourceCampaignName)
	}

	targetAssessment, err := dao.FindExistingAssessment(ctx, client, db, targetAssessmentName)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return fmt.Errorf("could not look up target assessment '%s': %w", targetAssessmentName, err)
	}
	if len(targetAssessment.Assessments.Nodes) == 0 {
		return fmt.Errorf("target assessment '%s' not found in database '%s'", targetAssessmentName, db)
	}
	targetAssessmentId := targetAssessment.Assessments.Nodes[0].Id

	// Collect and validate library test case IDs for the specific campaign
	libraryTestCaseIDs := []string{}
	for _, tc := range campaignToRestore.TestCases {
		if tc.LibraryTestCaseId != "" && tc.LibraryTestCaseId != "null" {
			libraryTestCaseIDs = append(libraryTestCaseIDs, tc.LibraryTestCaseId)
		}
	}

	if optionalParams.ForceEnvOnly {
		slog.WarnContext(ctx, "--force-env-only set, skipping library test case validation", "assessment-name", ad.Assessment.Name, "campaign-name", sourceCampaignName)
	} else {
		if err := validateLibraryTestCases(ctx, client, libraryTestCaseIDs, ad.TemplateAssessment); err != nil {
			return err
		}
	}

	// Collect tools for the specific campaign
	campaignToolsToReconcile := make(map[string]DefenseToolRef)
	for _, tc := range campaignToRestore.TestCases {
		for _, outcome := range tc.DefenseToolOutcomes {
			toolID := strconv.Itoa(outcome.DefenseToolId)
			if tool, ok := ad.IdToolsMap[toolID]; ok {
				campaignToolsToReconcile[tool.Key()] = tool
			}
		}
	}

	// Restrict the org map to the organizations used by this campaign
	campaignOrgMap := make(map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization, len(campaignToRestore.Organizations))
	for _, org := range campaignToRestore.Organizations {
		if orgDetail, ok := ad.OrgMap[org.Name]; ok {
			campaignOrgMap[org.Name] = orgDetail
		}
	}

	org_map, err := validateRestorePrerequisites(ctx, client, db, campaignOrgMap)
	if err != nil {
		return err
	}

	toolIdByKey, err := reconcileDefenseTools(ctx, client, db, campaignToolsToReconcile)
	if err != nil {
		return err
	}

	return restoreCampaigns(ctx, client, db, targetAssessmentId, targetAssessmentName, []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign{campaignToRestore}, org_map, toolIdByKey, ad.IdToolsMap, optionalParams)
}

func loadVatMetadata(md []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair, manifest Manifest, restoreInfo VatOpMetadata) []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair {
	for k, v := range AsVectrMetadataPairs(manifest, restoreInfo) {
		md = append(md, dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair{
			Key:   k,
			Value: v,
		})
	}
	return md
}

// createTemplateData builds a CreateTestCaseTemplateDataInput from a library
// test case. The returned []error is recoverable parsing discrepancies the
// caller can log and continue past; the returned error is fatal -- OrgMap is
// a resource vat itself manages and requires, so a missing organization here
// means our own data is inconsistent, not something to paper over.
func createTemplateData(template_test_case dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase) (dao.CreateTestCaseTemplateDataInput, []error, error) {
	if len(template_test_case.Organizations) == 0 {
		return dao.CreateTestCaseTemplateDataInput{}, nil, fmt.Errorf("test case %s (library id %s) has no organization set", template_test_case.Name, template_test_case.LibraryTestCaseId)
	}
	var errors []error
	ttc := dao.CreateTestCaseTemplateDataInput{
		LibraryTestCaseId: template_test_case.LibraryTestCaseId,
		Name:              template_test_case.Name,
		Description:       template_test_case.Description,
		Phase:             template_test_case.Phase.Name,
		Technique:         template_test_case.MitreId,
		// Tags:              []string{}, //handle below
		Organization: template_test_case.Organizations[0].Name,
		// Defenses:          []string{}, //handle below
		DetectionSteps:  template_test_case.DetectionGuidance,
		PreventionSteps: template_test_case.PreventionGuidance,
		References:      template_test_case.References,
		// RedTools:          []RedToolInput{}, //handle below
		OperatorGuidance: template_test_case.OperatorGuidance,
		UserContext:      template_test_case.UserContext,

		// RedTeamMetadata:   []MetadataKeyValuePairInput{}, //handle below
		// BlueTeamMetadata:  []MetadataKeyValuePairInput{}, //handle below
		// AttackAutomation:  &AttackAutomationInput{},      //handle below
		// TemplatePrefix:    "",                            //handle below
	}
	for _, tag := range template_test_case.Tags {
		ttc.Tags = append(ttc.Tags, tag.Name)
	}

	for _, defense := range template_test_case.DefensiveLayers {
		ttc.Defenses = append(ttc.Defenses, defense.Name)
	}
	for _, redtool := range template_test_case.RedTools {
		ttc.RedTools = append(ttc.RedTools, dao.RedToolInput{Name: redtool.Name})
	}
	for _, md := range template_test_case.Metadata {
		ttc.BlueTeamMetadata = append(ttc.BlueTeamMetadata, dao.MetadataKeyValuePairInput(md))
	}
	if template_test_case.AutomationCmd != "" {
		ttc.AttackAutomation, errors = buildAttackAutomationInput(&template_test_case)
	}
	// check for the prefix
	for _, md := range template_test_case.Metadata {
		if md.Key == "prefix" {
			ttc.TemplatePrefix = md.Value
			// There is a bug in the template test case create where if there is a prefix it will keep adding,
			// it onto the name, you gotta remove it to insert it.
			// #VECTRBUG
			ttc.Name = strings.TrimPrefix(template_test_case.Name, ttc.TemplatePrefix+" - ")
			break
		}
	}
	return ttc, errors, nil
}

func buildAttackAutomationInput[A AutomationArgumentTypes, PA pointerAutomationArgs[A], T Automator[A]](automator T) (*dao.AttackAutomationInput, []error) {
	errors := make([]error, 0, len(automator.GetAutomationArgument()))

	executor, ok := executorMap[automator.GetAutomationExecutor()]
	if !ok {
		errors = append(errors, fmt.Errorf("cmd: %s, unrecognized executor: %s, passing it through as-is (forwards compat)", automator.GetAutomationCmd(), automator.GetAutomationExecutor()))
		executor = dao.AttackAutomationExecutor(automator.GetAutomationExecutor())
	}
	cleanupExecutor, ok := executorMap[automator.GetAutomationCleanupExecutor()]
	if !ok {
		errors = append(errors, fmt.Errorf("cmd: %s, unrecognized cleanup executor: %s, passing it through as-is (forwards compat)", automator.GetAutomationCmd(), automator.GetAutomationCleanupExecutor()))
		cleanupExecutor = dao.AttackAutomationExecutor(automator.GetAutomationCleanupExecutor())
	}
	attackAutomation := &dao.AttackAutomationInput{
		Command:         automator.GetAutomationCmd(),
		Executor:        executor,
		CleanupCommand:  automator.GetAutomationCleanup(),
		CleanupExecutor: cleanupExecutor,
	}
	args := automator.GetAutomationArgument()
	for _, autoArg := range args {
		pointerAutoArg := PA(&autoArg)
		// set the default type to be a string, if it is set to path we will use that, else use the string
		argTypeDefault := dao.AutomationVarTypeString
		if dao.AutomationVarType(strings.ToUpper(pointerAutoArg.GetArgumentType())) == dao.AutomationVarTypePath {
			argTypeDefault = dao.AutomationVarTypePath
		} else {
			errors = append(errors, fmt.Errorf("cmd: %s, arugment: %s with the value: %s has the type: %s, resetting to %s", automator.GetAutomationCmd(), pointerAutoArg.GetArgumentKey(), pointerAutoArg.GetArgumentValue(), pointerAutoArg.GetArgumentType(), dao.AutomationVarTypeString))
		}
		attackAutomation.AttackVariables = append(attackAutomation.AttackVariables, dao.AttackAutomationVariable{
			InputName:  pointerAutoArg.GetArgumentKey(),
			InputValue: pointerAutoArg.GetArgumentValue(),
			Type:       argTypeDefault,
		})
	}
	return attackAutomation, errors
}

func ParseLibraryTestcasesByIdsError(e string) ([]string, error) {
	// Define the prefix to look for
	prefix := "The following IDs were not valid: "
	if !strings.HasPrefix(e, prefix) {
		return nil, fmt.Errorf("input string does not start with the expected prefix")
	}

	// Remove the prefix to get the IDs part
	idsPart := strings.TrimPrefix(e, prefix)
	ids := strings.Split(idsPart, ", ")

	var uuids []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		_, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("could not parse %s: %w", id, err)
		}
		uuids = append(uuids, id)
	}

	if len(uuids) == 0 {
		return nil, fmt.Errorf("no valid UUIDs found in the input string")
	}

	return uuids, nil
}
