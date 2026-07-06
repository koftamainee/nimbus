package raft

func (r *Raft) infof(msg string, args ...any) {
	r.logger.Info(msg, args...)
}

func (r *Raft) warnf(msg string, args ...any) {
	r.logger.Warn(msg, args...)
}
