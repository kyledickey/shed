package onepassword

import (
	"context"

	sdk "github.com/1password/onepassword-sdk-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const scope = "github.com/kyledickey/shed/internal/onepassword"

// traced runs fn, a call to 1Password, in a client span named after op. The
// SDK makes its HTTP requests inside its WebAssembly core, out of reach of
// an instrumented HTTP client. Spans carry no arguments, results, or error
// text, which could hold variable values or the token.
//
// fn never sees ctx's cancellation: a request the SDK's core abandons midway
// corrupts the core, which every client shares, and each later call then
// fails with "out of bounds memory access" until shed restarts.
func traced[T any](ctx context.Context, op string, fn func(context.Context) (T, error)) (T, error) {
	ctx, span := otel.Tracer(scope).Start(context.WithoutCancel(ctx), "1password "+op, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	v, err := fn(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "")
	}
	return v, err
}

// tracedAPI traces every call of an api.
type tracedAPI struct{ api api }

func (t tracedAPI) ListVaults(ctx context.Context) ([]sdk.VaultOverview, error) {
	return traced(ctx, "list vaults", t.api.ListVaults)
}

func (t tracedAPI) CreateItem(ctx context.Context, params sdk.ItemCreateParams) (sdk.Item, error) {
	return traced(ctx, "create item", func(ctx context.Context) (sdk.Item, error) { return t.api.CreateItem(ctx, params) })
}

func (t tracedAPI) GetItems(ctx context.Context, vaultID string, itemIDs []string) (sdk.ItemsGetAllResponse, error) {
	return traced(ctx, "get items", func(ctx context.Context) (sdk.ItemsGetAllResponse, error) {
		return t.api.GetItems(ctx, vaultID, itemIDs)
	})
}

func (t tracedAPI) PutItem(ctx context.Context, item sdk.Item) (sdk.Item, error) {
	return traced(ctx, "put item", func(ctx context.Context) (sdk.Item, error) { return t.api.PutItem(ctx, item) })
}

func (t tracedAPI) ArchiveItem(ctx context.Context, vaultID, itemID string) error {
	_, err := traced(ctx, "archive item", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, t.api.ArchiveItem(ctx, vaultID, itemID)
	})
	return err
}
