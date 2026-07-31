package engine

// In-pipeline agent stages (graph_json v2) run synchronously: the cicd
// PipelineOrchestrator dispatches AgentRuns via AgentService and advances on
// the AgentService terminal hook. Build-event agent triggers
// (artifact_ready / distribution_finished) remain asynchronous and never
// mutate BuildRun.status. This file is kept as a pointer to both mechanisms.
