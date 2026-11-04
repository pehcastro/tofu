package contract

import "context"

type SubAgentKey struct{}

func SubAgentAsking(ctx context.Context) string {
	id, _ := ctx.Value(SubAgentKey{}).(string)
	return id
}

type ShellOwnerKey struct{}

func ShellOwnerFrom(ctx context.Context) string {
	owner, _ := ctx.Value(ShellOwnerKey{}).(string)
	return owner
}
