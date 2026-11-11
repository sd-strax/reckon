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

// projection is the column list every CloudTrail query reads: the scalars as
// stored, each struct cast to JSON (which Athena engine v3 renders as an
// object keyed by field name), and the accountid partition under a name of
// its own, which rowToEvent uses only to supply a missing cloud.account.
// rowToEvent reverses it.
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

// lakeAccountColumn is the result name of the projected accountid partition.
const lakeAccountColumn = "lake_accountid"

// predicateOnlyColumns are table columns the WHERE clause reads that the
// projection does not.
var predicateOnlyColumns = []string{"region"}

func jsonColumn(name string) column {
	return column{name, "CAST(" + name + " AS JSON) AS " + name, name}
}

// selectList is the projection as SQL.
func selectList() string {
	exprs := make([]string, len(projection))
	for i, c := range projection {
		exprs[i] = c.expr
	}
	return strings.Join(exprs, ", ")
}

// projectedNames are the result column names, in order.
func projectedNames() []string {
	out := make([]string, len(projection))
	for i, c := range projection {
		out[i] = c.name
	}
	return out
}

// requiredColumns are the table columns (top-level names, partitions
// included) the queries read; health checks the table has every one.
func requiredColumns() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range projection {
		if !seen[c.source] {
			seen[c.source] = true
			out = append(out, c.source)
		}
	}
	for _, c := range predicateOnlyColumns {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
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
// cloudtrail.resource_history (history true). Every value is a parameter;
// only the validated table reference and fixed predicates are SQL text.
// History selects the newest limit rows; the caller reverses them to oldest
// first, so a long timeline keeps its latest changes.
func activityQuery(db, table string, accounts []string, c cloudCall, history bool) (sqlQuery, error) {
	ref, err := tableRef(db, table)
	if err != nil {
		return sqlQuery{}, err
	}
	q, err := activityWhere(accounts, c, history)
	if err != nil {
		return sqlQuery{}, err
	}
	var b strings.Builder
	b.WriteString("SELECT " + selectList() + "\n")
	b.WriteString("FROM " + ref + "\n")
	b.WriteString("WHERE " + q.clause() + "\n")
	b.WriteString(`ORDER BY "time" DESC` + "\n")
	b.WriteString("LIMIT ?")
	return sqlQuery{sql: b.String(), params: append(q.params, strconv.Itoa(c.limit))}, nil
}

// activityWhere is the WHERE predicates of an activity (history false) or
// history (history true) query and their parameters, in order. The queries
// and health's probe both build from it.
func activityWhere(accounts []string, c cloudCall, history bool) (*queryBuilder, error) {
	if history && !c.resource.given() {
		return nil, fatal("resource history needs a resource uid or name")
	}
	q := &queryBuilder{}
	q.add("time_dt BETWEEN from_iso8601_timestamp(?) AND from_iso8601_timestamp(?)",
		strLit(c.from.Format(time.RFC3339)), strLit(c.to.Format(time.RFC3339)))
	// The table is API Activity; the lake may file some IAM operations under
	// another class (3001 Account Change), which this predicate leaves out —
	// a check for the first live run (adr/0322).
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
		if c.principalName != "" {
			n := strLit(c.principalName)
			q.add("(actor.user.uid = ? OR actor.session.issuer = ? OR actor.user.name = ?)", n, n, n)
		}
		if c.credential != "" {
			k := strLit(c.credential)
			q.add("(actor.user.credential_uid = ? OR actor.session.credential_uid = ?)", k, k)
		}
		if c.accountUID != "" {
			addAccount(q, c.accountUID)
		}
		if c.operation != "" {
			q.add("api.operation = ?", strLit(c.operation))
		}
		if c.service != "" {
			q.add("api.service.name = ?", strLit(c.service))
		}
		switch c.outcome {
		case "SUCCESS":
			q.add("status_id = 1")
		case "FAILURE":
			q.add("status_id = 2")
		}
	}
	addResource(q, c.resource)
	if history {
		q.add("activity_id IN (1, 3, 4)")
	}
	return q, nil
}

// clause is the predicates joined as a WHERE clause body.
func (q *queryBuilder) clause() string { return strings.Join(q.where, "\n  AND ") }

// probeCall is one synthetic call the probe plans.
type probeCall struct {
	call    cloudCall
	history bool
}

// probeCalls are synthetic calls that between them exercise every predicate
// activityWhere can emit: an activity call with every filter and a resource
// named with its type, region and account; an activity call with a region
// and no resource name (the event-region branch); and a history call on a
// uid-identified resource (the uid branch and the activity filter). Values
// are placeholders: the probe matches nothing.
func probeCalls() []probeCall {
	epoch := time.Unix(0, 0).UTC()
	const v = "probe"
	return []probeCall{
		{cloudCall{
			principalName: v, credential: v, accountUID: v, operation: v, service: v, outcome: "SUCCESS",
			resource: resourceScope{name: v, typ: v, region: v, accountUID: v}, from: epoch, to: epoch,
		}, false},
		{cloudCall{resource: resourceScope{region: v}, from: epoch, to: epoch}, false},
		{cloudCall{resource: resourceScope{uid: v}, from: epoch, to: epoch}, true},
	}
}

// probeAccounts stands in for configured account_uids, so the probe plans
// the accountid IN predicate.
var probeAccounts = []string{"000000000000"}

// probeQuery is health's probe: the queries' own projection and the WHERE
// predicates of every query shape (probeCalls, built by activityWhere)
// against the configured table, behind `false` so it matches nothing. Athena
// resolves every column the queries read — projected or only filtered on —
// and checks the caller may read the table, so a missing column, a type a
// predicate or the projection cannot use, or a denied table fails here. It is
// built from the same code as the queries, so it cannot drift from them.
func probeQuery(db, table string) (sqlQuery, error) {
	ref, err := tableRef(db, table)
	if err != nil {
		return sqlQuery{}, err
	}
	where := []string{"false"}
	var params []string
	for _, p := range probeCalls() {
		q, err := activityWhere(probeAccounts, p.call, p.history)
		if err != nil {
			return sqlQuery{}, err
		}
		where = append(where, "("+q.clause()+")")
		params = append(params, q.params...)
	}
	return sqlQuery{
		sql:    "SELECT " + selectList() + "\nFROM " + ref + "\nWHERE " + strings.Join(where, "\n  AND ") + "\nLIMIT 0",
		params: params,
	}, nil
}

// addAccount filters on the account the call ran in through the accountid
// partition: the lake partitions each event by its recording account, which
// is that account, and the partition is present on every row, where
// cloud.account may be absent (the OCSF 1.1 CloudTrail mapping can carry the
// account only under actor.user.account). Filtering cloud.account.uid too
// would drop such rows and answer a false empty.
func addAccount(q *queryBuilder, uid string) {
	q.add("accountid = ?", strLit(uid))
}

// addResource adds the resource predicates the table can express, matched
// on a resources[] entry with any_match (Athena engine v3 expands an UNNEST
// of an array of rows into one column per field). A uid is the provider's
// unique identifier and carries its own owner and place, so a resource given
// by uid is matched on the uid alone: no name, account, region or other
// scope narrows it. A resource given by name is matched on one element
// carrying the name and, when given, the type and region — the element's own
// region, which its identity uses, not the event's — under the account
// given; the region partition is filtered too, since a call on a resource
// is logged in the resource's region. Zone, group and namespace are checked
// on the same element after the fetch (scope.go). A region given with
// neither uid nor name filters the event's region.
func addResource(q *queryBuilder, r resourceScope) {
	if r.uid != "" {
		q.add("any_match(resources, r -> r.uid = ?)", strLit(r.uid))
		return
	}
	if r.name != "" {
		conds, args := []string{"r.name = ?"}, []string{strLit(r.name)}
		if r.typ != "" {
			conds, args = append(conds, "r.type = ?"), append(args, strLit(r.typ))
		}
		if r.region != "" {
			conds, args = append(conds, "r.region = ?"), append(args, strLit(r.region))
		}
		q.add("any_match(resources, r -> "+strings.Join(conds, " AND ")+")", args...)
	}
	if r.accountUID != "" {
		addAccount(q, r.accountUID)
	}
	if r.region != "" {
		if r.name == "" {
			q.add("cloud.region = ?", strLit(r.region))
		}
		q.add("region = ?", strLit(r.region))
	}
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
