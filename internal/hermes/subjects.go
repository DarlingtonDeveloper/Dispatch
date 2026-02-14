package hermes

const (
	SubjectTaskRequest   = "swarm.task.request"
	SubjectAgentStarted  = "swarm.agent.*.started"
	SubjectAgentStopped  = "swarm.agent.*.stopped"
	SubjectDispatchStats = "swarm.dispatch.stats"

	StreamName   = "DISPATCH_EVENTS"
	StreamMaxAge = "720h" // 30 days
)

func SubjectTaskCreated(taskID string) string    { return "swarm.task." + taskID + ".created" }
func SubjectTaskAssigned(taskID string) string    { return "swarm.task." + taskID + ".assigned" }
func SubjectTaskStarted(taskID string) string     { return "swarm.task." + taskID + ".started" }
func SubjectTaskCompleted(taskID string) string   { return "swarm.task." + taskID + ".completed" }
func SubjectTaskFailed(taskID string) string      { return "swarm.task." + taskID + ".failed" }
func SubjectTaskTimeout(taskID string) string     { return "swarm.task." + taskID + ".timeout" }
func SubjectTaskRetry(taskID string) string       { return "swarm.task." + taskID + ".retry" }
func SubjectTaskDLQ(taskID string) string         { return "swarm.task." + taskID + ".dlq" }
func SubjectTaskReassigned(taskID string) string  { return "swarm.task." + taskID + ".reassigned" }
func SubjectTaskProgress(taskID string) string    { return "swarm.task." + taskID + ".progress" }
func SubjectTaskUnmatched(taskID string) string   { return "swarm.task." + taskID + ".unmatched" }

func SubjectDispatchAssigned(taskID string) string  { return "swarm.dispatch." + taskID + ".assigned" }
func SubjectDispatchCompleted(taskID string) string { return "swarm.dispatch." + taskID + ".completed" }
func SubjectDispatchOversight(taskID string) string { return "swarm.dispatch." + taskID + ".oversight" }
