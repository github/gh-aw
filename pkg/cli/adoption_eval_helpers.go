package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

type ghAPIGetArrayFunc func(context.Context, string, string) ([]map[string]any, error)

func isLatestCloseByBot(ctx context.Context, number int, repo string, getEvents ghAPIGetArrayFunc) (bool, error) {
	events, err := getEvents(ctx, fmt.Sprintf("issues/%d/events", number), repo)
	if err != nil {
		return false, err
	}
	for _, entry := range slices.Backward(events) {
		event := adoptionValue[string](entry["event"])
		if event != "closed" {
			continue
		}
		actor := adoptionValue[map[string]any](entry["actor"])
		login := adoptionValue[string](actor["login"])
		if login == "" {
			return false, errors.New("latest close actor is unavailable")
		}
		return !isNonBotActor(actor), nil
	}
	return false, fmt.Errorf("no close event found for %s#%d", repo, number)
}
