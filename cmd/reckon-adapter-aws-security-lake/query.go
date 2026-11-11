package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// identRE is the closed character set a database or table name must match
// before it is interpolated: names cannot be Athena execution parameters, so
// they are validated and double-quoted instead (adr/0322).
var identRE = regexp.MustCompile(`^[a-z0-9_]+$`)

const (
	defaultLimit = 200
	maxLimit     = 1000
)

// column is one projected column: the table column it reads, the SELECT
// expression, and the name the result carries.
type column struct{ source, expr, name string }

// projection is every column a CloudTrail query may read, in order: the
// scalars as stored, each struct cast to JSON (which Athena engine v3
// renders as an object keyed by field name), and the accountid column under
// a name of its own, which rowToEvent uses only to supply a missing
// cloud.account. A query projects the ones the table carries
// (tableSchema.projection); rowToEvent reverses it.
var projection = []column{
	{"time", `"time"`, "time"},
	{"time_dt", "time_dt", "time_dt"},
	{"activity_id", "activity_id", "activity_id"},
	{"activity_name", "activity_name", "activity_name"},
	{"status", "status", "status"},
	{"status_id", "status_id", "status_id"},
	jsonColumn("actor"),
	jsonColumn("api"),
	jsonColumn("cloud"),
	jsonColumn("resources"),
	jsonColumn("src_endpoint"),
	jsonColumn("http_request"),
	jsonColumn("metadata"),
	jsonColumn("unmapped"),
	{"severity_id", "severity_id", "severity_id"},
	{"class_uid", "class_uid", "class_uid"},
	{"category_uid", "category_uid", "category_uid"},
	{"type_uid", "type_uid", "type_uid"},
	{"accountid", "accountid AS " + lakeAccountColumn, lakeAccountColumn},
}

// lakeAccountColumn is the result name of the projected accountid column.
const lakeAccountColumn = "lake_accountid"

// predicateOnlyColumns are table columns the WHERE clause may read that the
// projection does not.
var predicateOnlyColumns = []string{"region"}

func jsonColumn(name string) column {
	return column{name, "CAST(" + name + " AS JSON) AS " + name, name}
}

// projection is the projection's columns the table carries, in order.
func (s *tableSchema) projection() []column {
	var out []column
	for _, c := range projection {
		if s.has(c.source) {
			out = append(out, c)
		}
	}
	return out
}

// selectList is the table's projection as SQL.
func (s *tableSchema) selectList() string {
	cols := s.projection()
	exprs := make([]string, len(cols))
	for i, c := range cols {
		exprs[i] = c.expr
	}
	return strings.Join(exprs, ", ")
}

// resourceScope is the resource a call names, with every scope key the
// binding forwards (examples/capability/exposed-bucket.yaml).
type resourceScope struct {
	uid, name, provider, typ, accountUID, region, zone, group, namespace string
}

func (r resourceScope) given() bool { return r.uid != "" || r.name != "" }

// cloudCall is one decoded activity or history call.
type cloudCall struct {
	principalName   string
	credential      string
	resource        resourceScope
	accountUID      string
	accountProvider string
	operation       string
	service         string
	outcome         string
	limit           int
	from, to        time.Time
}

// foreignProvider reports whether the call names a provider other than the
// lake's: every row in the lake is AWS, so such a call has no answer here.
func (c cloudCall) foreignProvider() bool {
	for _, p := range []string{c.resource.provider, c.accountProvider} {
		if p != "" && !strings.EqualFold(p, "aws") {
			return true
		}
	}
	return false
}

// parseCall decodes binding-rendered params. Values may arrive as strings or
// JSON numbers; unknown keys (the injected verb among them) are ignored.
func parseCall(params map[string]any) (cloudCall, error) {
	principal, account, res := sub(params, "principal"), sub(params, "account"), sub(params, "resource")
	c := cloudCall{
		principalName:   str(principal, "name"),
		credential:      str(principal, "credential"),
		accountUID:      str(account, "uid"),
		accountProvider: str(account, "provider"),
		operation:       str(params, "operation"),
		service:         str(params, "service"),
		outcome:         strings.ToUpper(str(params, "outcome")),
		resource: resourceScope{
			uid: str(res, "uid"), name: str(res, "name"), provider: str(res, "provider"),
			typ: str(res, "type"), accountUID: str(res, "account_uid"), region: str(res, "region"),
			zone: str(res, "zone"), group: str(res, "group"), namespace: str(res, "namespace"),
		},
	}
	switch c.outcome {
	case "", "ALL", "SUCCESS", "FAILURE":
	default:
		return c, fatal("outcome must be SUCCESS, FAILURE or ALL, got " + strconv.Quote(c.outcome))
	}
	limit, err := parseLimit(params["limit"])
	if err != nil {
		return c, err
	}
	c.limit = limit
	c.from, c.to, err = parseWindow(sub(params, "window"))
	return c, err
}

// parseLimit reads limit as a number or a numeric string and clamps it to
// 1..maxLimit; absent is the verb's default.
func parseLimit(v any) (int, error) {
	n := defaultLimit
	switch x := v.(type) {
	case nil:
	case float64:
		if x != math.Trunc(x) {
			return 0, fatal("limit must be an integer")
		}
		n = int(math.Max(math.Min(x, maxLimit), 0))
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			break
		}
		i, err := strconv.Atoi(s)
		if err != nil {
			return 0, fatal("limit must be an integer, got " + strconv.Quote(s))
		}
		n = i
	default:
		return 0, fatal(fmt.Sprintf("limit must be an integer, got %T", v))
	}
	return min(max(n, 1), maxLimit), nil
}

// now is the clock a half-open window closes on; tests replace it.
var now = func() time.Time { return time.Now().UTC() }

// parseWindow reads window.from / window.to (RFC 3339). A missing start, or
// the zero time the binding renders when the caller gave no window, is
// refused: a control-plane query without a start would scan the lake. A
// missing or zero end is open: the window runs to now.
func parseWindow(w map[string]any) (time.Time, time.Time, error) {
	from, err := parseBound(str(w, "from"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if from.IsZero() {
		return time.Time{}, time.Time{}, fatal(windowRequired)
	}
	to, err := parseBound(str(w, "to"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if to.IsZero() {
		to = now()
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, fatal("window.from is after window.to")
	}
	return from, to, nil
}

const windowRequired = "a control-plane query needs a window"

// parseBound parses one bound; absent or the zero time is the zero time.
func parseBound(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fatal(windowRequired + ": " + err.Error())
	}
	if t.IsZero() {
		return time.Time{}, nil
	}
	return t.UTC(), nil
}

// sqlQuery is a statement with its positional parameters, each already
// rendered as the SQL literal Athena substitutes for the matching `?`.
type sqlQuery struct {
	sql    string
	params []string
}

// queryBuilder accumulates WHERE predicates and their parameters in order.
type queryBuilder struct {
	where  []string
	params []string
}

func (q *queryBuilder) add(pred string, params ...string) {
	q.where = append(q.where, pred)
	q.params = append(q.params, params...)
}

// tableRef validates and quotes the configured database and table.
func tableRef(db, table string) (string, error) {
	if !identRE.MatchString(db) {
		return "", fatal("glue database name " + strconv.Quote(db) + " is not [a-z0-9_]+")
	}
	if !identRE.MatchString(table) {
		return "", fatal("table name " + strconv.Quote(table) + " is not [a-z0-9_]+")
	}
	return `"` + db + `"."` + table + `"`, nil
}

// activityQuery renders cloudtrail.api_activity (history false) or
// cloudtrail.resource_history (history true) against the table's schema.
// Every value is a parameter; only the validated table reference and fixed
// predicates are SQL text. History selects the newest limit rows; the caller
// reverses them to oldest first, so a long timeline keeps its latest
// changes. A call needing a capability the table cannot express is declined
// with FALLTHROUGH (activityWhere).
func activityQuery(s *tableSchema, db, table string, accounts []string, c cloudCall, history bool) (sqlQuery, error) {
	ref, err := tableRef(db, table)
	if err != nil {
		return sqlQuery{}, err
	}
	q, err := activityWhere(s, accounts, c, history)
	if err != nil {
		return sqlQuery{}, err
	}
	var b strings.Builder
	b.WriteString("SELECT " + s.selectList() + "\n")
	b.WriteString("FROM " + ref + "\n")
	b.WriteString("WHERE " + q.clause() + "\n")
	b.WriteString(`ORDER BY "time" DESC` + "\n")
	b.WriteString("LIMIT ?")
	return sqlQuery{sql: b.String(), params: append(q.params, strconv.Itoa(c.limit))}, nil
}

// activityWhere is the WHERE predicates of an activity (history false) or
// history (history true) query and their parameters, in order, each filter
// over the candidate fields the table carries (adr/0335). A filter with no
// candidate present declines the call with FALLTHROUGH naming the absent
// fields: a condition is never dropped, since a limited query without it
// answers a page that may miss what was asked for. The queries and health's
// probe both build from it.
func activityWhere(s *tableSchema, accounts []string, c cloudCall, history bool) (*queryBuilder, error) {
	if history && !c.resource.given() {
		return nil, fatal("resource history needs a resource uid or name")
	}
	if history && len(s.present(historyFields)) == 0 {
		return nil, declined(capHistory, historyFields)
	}
	q := &queryBuilder{}
	q.add("time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)",
		strLit(c.from.Format(time.RFC3339)), strLit(c.to.Format(time.RFC3339)))
	// The table is API Activity; the lake may file some IAM operations under
	// another class (3001 Account Change), which this predicate leaves out
	// (adr/0322).
	q.add("class_uid = 6003")
	if len(accounts) > 0 {
		marks := make([]string, len(accounts))
		lits := make([]string, len(accounts))
		for i, a := range accounts {
			marks[i], lits[i] = "?", strLit(a)
		}
		q.add("accountid IN ("+strings.Join(marks, ", ")+")", lits...)
	}
	if !history {
		for _, f := range []struct {
			capName    string
			candidates []string
			value      string
		}{
			{capPrincipal, principalFields, c.principalName},
			{capCredential, credentialFields, c.credential},
		} {
			if f.value == "" {
				continue
			}
			if err := q.anyOf(s, f.capName, f.candidates, f.value); err != nil {
				return nil, err
			}
		}
		if c.accountUID != "" {
			addAccount(q, c.accountUID)
		}
		for _, f := range []struct {
			capName    string
			candidates []string
			value      string
		}{
			{capOperation, operationFields, c.operation},
			{capService, serviceFields, c.service},
		} {
			if f.value == "" {
				continue
			}
			if err := q.anyOf(s, f.capName, f.candidates, f.value); err != nil {
				return nil, err
			}
		}
		if err := addOutcome(q, s, c.outcome); err != nil {
			return nil, err
		}
	}
	if err := addResource(q, s, c.resource); err != nil {
		return nil, err
	}
	if history {
		q.add("activity_id IN (1, 3, 4)")
	}
	return q, nil
}

// anyOf adds `candidate = value` over the candidates the table carries,
// joined with OR; with none present the call is declined.
func (q *queryBuilder) anyOf(s *tableSchema, capName string, candidates []string, value string) error {
	have := s.present(candidates)
	if len(have) == 0 {
		return declined(capName, candidates)
	}
	conds := make([]string, len(have))
	args := make([]string, len(have))
	for i, f := range have {
		conds[i], args[i] = f+" = ?", strLit(value)
	}
	if len(conds) == 1 {
		q.add(conds[0], args...)
		return nil
	}
	q.add("("+strings.Join(conds, " OR ")+")", args...)
	return nil
}

// addOutcome filters SUCCESS or FAILURE on status_id where the table has it,
// else on the status string compared without case (the 2.0 table carries
// only "Success"/"Failure").
func addOutcome(q *queryBuilder, s *tableSchema, outcome string) error {
	if outcome != "SUCCESS" && outcome != "FAILURE" {
		return nil
	}
	switch {
	case s.has("status_id"):
		if outcome == "SUCCESS" {
			q.add("status_id = 1")
		} else {
			q.add("status_id = 2")
		}
	case s.has("status"):
		q.add("lower(status) = '" + strings.ToLower(outcome) + "'")
	default:
		return declined(capOutcome, outcomeFields)
	}
	return nil
}

// clause is the predicates joined as a WHERE clause body.
func (q *queryBuilder) clause() string { return strings.Join(q.where, "\n  AND ") }

// probeCall is one synthetic call the probe plans.
type probeCall struct {
	call    cloudCall
	history bool
}

// probeCalls are synthetic calls that between them send every predicate
// activityWhere can emit against the table's schema, each at least once:
// an activity call with every filter the table can express (outcome
// SUCCESS) and a resource named with its type, region, zone, group,
// namespace and account where those are expressible; an activity call with
// a region and no resource name (the event-region branch); an activity call
// on an unnamed resource scoped by every scope the table carries; an
// activity call on a uid-identified
// resource with outcome FAILURE; and history calls on a uid- and, where the
// table carries names, a name-identified resource (the activity filter). A
// capability the table cannot express is left out, so the probe plans only
// what a call can send. Values are placeholders: the probe matches nothing.
func probeCalls(s *tableSchema) []probeCall {
	epoch := time.Unix(0, 0).UTC()
	const v = "probe"
	expressible := func(candidates []string) bool { return len(s.present(candidates)) > 0 }
	only := func(ok bool, value string) string {
		if ok {
			return value
		}
		return ""
	}
	// scoped is every element scope the table carries: type, zone, group
	// (by whichever of its name and uid the table has) and namespace.
	scoped := resourceScope{
		typ:       only(s.has(resourceTypeField), v),
		zone:      only(s.has(postScopeFields[0]), v),
		group:     only(groupMissing(s) == nil, v),
		namespace: only(s.has(postScopeFields[2]), v),
	}
	named := resourceScope{accountUID: v}
	if s.has(resourceNameField) {
		named = scoped
		named.name, named.accountUID = v, v
		named.region = only(s.has(resourceRegionField), v)
	}
	calls := []probeCall{{cloudCall{
		principalName: only(expressible(principalFields), v),
		credential:    only(expressible(credentialFields), v),
		accountUID:    v,
		operation:     only(expressible(operationFields), v),
		service:       only(expressible(serviceFields), v),
		outcome:       only(expressible(outcomeFields), "SUCCESS"),
		resource:      named, from: epoch, to: epoch,
	}, false}}
	if expressible(eventRegionFields) {
		calls = append(calls, probeCall{cloudCall{resource: resourceScope{region: v}, from: epoch, to: epoch}, false})
	}
	if scoped != (resourceScope{}) {
		calls = append(calls, probeCall{cloudCall{resource: scoped, from: epoch, to: epoch}, false})
	}
	history := expressible(historyFields)
	if s.has(resourceUIDFields[0]) {
		calls = append(calls, probeCall{cloudCall{
			outcome: only(expressible(outcomeFields), "FAILURE"), resource: resourceScope{uid: v}, from: epoch, to: epoch,
		}, false})
		if history {
			calls = append(calls, probeCall{cloudCall{resource: resourceScope{uid: v}, from: epoch, to: epoch}, true})
		}
	}
	if history && s.has(resourceNameField) {
		calls = append(calls, probeCall{cloudCall{resource: named, from: epoch, to: epoch}, true})
	}
	return calls
}

// probeAccounts stands in for configured account_uids, so the probe plans
// the accountid IN predicate.
var probeAccounts = []string{"000000000000"}

// probeQuery is health's probe: the table's projection and the WHERE
// predicates of every query shape the table can express (probeCalls, built
// by activityWhere) against the configured table, behind `false` so it
// matches nothing. Athena resolves every column the queries read —
// projected or only filtered on — and checks the caller may read the table,
// so a column the schema reported but the table lacks, a type a predicate or
// the projection cannot use, or a denied table fails here. It is built from
// the same code as the queries, so it cannot drift from them.
func probeQuery(s *tableSchema, db, table string) (sqlQuery, error) {
	ref, err := tableRef(db, table)
	if err != nil {
		return sqlQuery{}, err
	}
	where := []string{"false"}
	var params []string
	for _, p := range probeCalls(s) {
		q, err := activityWhere(s, probeAccounts, p.call, p.history)
		if err != nil {
			return sqlQuery{}, err
		}
		where = append(where, "("+q.clause()+")")
		params = append(params, q.params...)
	}
	return sqlQuery{
		sql:    "SELECT " + s.selectList() + "\nFROM " + ref + "\nWHERE " + strings.Join(where, "\n  AND ") + "\nLIMIT 0",
		params: params,
	}, nil
}

// addAccount filters on the account the call ran in through the accountid
// column: the lake files each event under its recording account, which is
// that account, and the column is present on every row, where
// cloud.account may be absent (the 2.0 table's cloud carries no account).
// Filtering cloud.account.uid too would drop such rows and answer a false
// empty.
func addAccount(q *queryBuilder, uid string) {
	q.add("accountid = ?", strLit(uid))
}

// addResource adds the resource predicates, matched on a resources[] entry
// with any_match (Athena engine v3 expands an UNNEST of an array of rows
// into one column per field). A uid is the provider's unique identifier and
// carries its own owner and place, so a resource given by uid is matched on
// the uid alone: no name, account, region or other scope narrows it, and a
// table without resources[].uid declines it — a name given beside it is no
// substitute for the identifier. Otherwise the resource's identity and
// scope are conditions on ONE element (resourceElement): the name, the type,
// the element's own region (beside a name, which its identity uses, not the
// event's), the zone, the group and the namespace, so the query's limit
// applies to rows that match and two elements cannot together satisfy it;
// the account given filters the accountid column, and beside a name the
// region column is filtered too where the table has it, since a call on a
// resource is logged in the resource's region. A region given with neither
// uid nor name filters the region the event was logged in: the region column
// where the table has it, else cloud.region, never both. A field the table
// lacks for any of these declines the call: dropping the condition would
// widen the match.
func addResource(q *queryBuilder, s *tableSchema, r resourceScope) error {
	if r.uid != "" {
		if !s.has(resourceUIDFields[0]) {
			return declined(capResourceUID, resourceUIDFields)
		}
		q.add("any_match(resources, r -> r.uid = ?)", strLit(r.uid))
		return nil
	}
	conds, args, capName, missing := resourceElement(s, r)
	if len(missing) > 0 {
		return declined(capName, missing)
	}
	if len(conds) > 0 {
		q.add("any_match(resources, r -> "+strings.Join(conds, " AND ")+")", args...)
	}
	if r.accountUID != "" {
		addAccount(q, r.accountUID)
	}
	if r.region == "" {
		return nil
	}
	if r.name == "" {
		have := s.present(eventRegionFields)
		if len(have) == 0 {
			return declined(capEventRegion, eventRegionFields)
		}
		q.add(have[0]+" = ?", strLit(r.region))
		return nil
	}
	if s.has("region") {
		q.add("region = ?", strLit(r.region))
	}
	return nil
}

// resourceElement is the conditions one resources[] element must meet for
// the resource a call names without a uid — its name and every scope
// given, each where the table carries the field — and their parameters. When
// a field is absent, missing names the absent fields and capName the first
// capability they belong to; the call is then declined.
func resourceElement(s *tableSchema, r resourceScope) (conds, args []string, capName string, missing []string) {
	need := func(cap string, fields ...string) bool {
		absent := s.absent(fields)
		if len(absent) > 0 && capName == "" {
			capName = cap
		}
		missing = append(missing, absent...)
		return len(absent) == 0
	}
	add := func(cond, value string, n int) {
		conds = append(conds, cond)
		for range n {
			args = append(args, strLit(value))
		}
	}
	if r.name != "" && need(capResourceName, resourceNameField) {
		add("r.name = ?", r.name, 1)
	}
	if r.typ != "" && need(capResourceType, resourceTypeField) {
		add("r.type = ?", r.typ, 1)
	}
	if r.name != "" && r.region != "" && need(capResourceRegion, resourceRegionField) {
		add("r.region = ?", r.region, 1)
	}
	if r.zone != "" && need(capResourceScope, postScopeFields[0]) {
		add("r.zone = ?", r.zone, 1)
	}
	if r.group != "" {
		// The group matches by its name or its uid (scope.go). group is a
		// reserved word in Athena, so it is quoted.
		if gm := groupMissing(s); len(gm) > 0 {
			if capName == "" {
				capName = capResourceScope
			}
			missing = append(missing, gm...)
		} else {
			var conds []string
			for _, f := range s.present(groupFields) {
				conds = append(conds, `r."group".`+strings.TrimPrefix(f, "resources[].group.")+" = ?")
			}
			if len(conds) == 1 {
				add(conds[0], r.group, 1)
			} else {
				add("("+strings.Join(conds, " OR ")+")", r.group, len(conds))
			}
		}
	}
	if r.namespace != "" && need(capResourceScope, postScopeFields[2]) {
		add("r.namespace = ?", r.namespace, 1)
	}
	return conds, args, capName, missing
}

// strLit renders s as the SQL string literal Athena substitutes for a `?`:
// an execution parameter is spliced in as written, so a string travels
// single-quoted with each embedded quote doubled.
func strLit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// sub returns params[key] as an object, nil when absent or not one.
func sub(params map[string]any, key string) map[string]any {
	m, _ := params[key].(map[string]any)
	return m
}

// str returns params[key] as a trimmed string; a JSON number is formatted
// without an exponent.
func str(params map[string]any, key string) string {
	switch v := params[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
