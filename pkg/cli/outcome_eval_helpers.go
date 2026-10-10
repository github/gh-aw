package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

type ghAPIGetArrayFunc func(context.Context, string, string) ([]map[string]any, error)

func countHumanComments(comments []map[string]any) int {
	count := 0
	for _, comment := range comments {
		if isHumanComment(comment) {
			count++
		}
	}
	return count
}

func countHumanCommentsAfter(comments []map[string]any, createdAt string) int {
	count := 0
	for _, comment := range comments {
		commentCreatedAt, _ := comment["created_at"].(string)
		if commentCreatedAt > createdAt && isHumanComment(comment) {
			count++
		}
	}
	return count
}

func isHumanComment(comment map[string]any) bool {
	return isNonBotActor(comment["user"])
}

func isLatestCloseByBot(ctx context.Context, number int, repo string, getEvents ghAPIGetArrayFunc) (bool, error) {
	events, err := getEvents(ctx, fmt.Sprintf("issues/%d/events", number), repo)
	if err != nil {
		return false, err
	}
	for i := range slices.Backward(events) {
		event, _ := events[i]["event"].(string)
		if event != "closed" {
			continue
		}
		actor, _ := events[i]["actor"].(map[string]any)
		login, _ := actor["login"].(string)
		if login == "" {
			return false, errors.New("latest close actor is unavailable")
		}
		return !isNonBotActor(actor), nil
	}
	return false, fmt.Errorf("no close event found for %s#%d", repo, number)
}
