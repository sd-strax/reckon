// Package agentapi holds the shapes the agent and the backend exchange over
// HTTP (adr/0278). The backend serves these types and the agent decodes them,
// so the compiler holds the two sides to one definition.
//
// It imports no engine package (aggregate, server, action, capability,
// temporal, knowledge; adr/0385), so the agent and the sidecar can use it.
// The engine packages that owned these types keep aliases of them, so engine
// code is unchanged.
//
// Some of these types are also the stored event format: TargetSpec,
// QuerySpec, ConsultedSOP and ConsultedSimilarInvestigation are written into
// the aggregate's events as they are. A change to one of their JSON tags
// changes how old events replay, so it is an event schema change, not only a
// wire change.
package agentapi
