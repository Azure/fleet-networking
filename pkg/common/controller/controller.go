package controller

import "context"

type MemberController interface {
	Join(ctx context.Context) error

	Leave(ctx context.Context) error
}
