import { useEffect } from 'react';

import { type Story, storiesApi } from '../api/stories';
import { hasProcessingMedia, runStoryPolling } from '../screens/stories/storyPolling';

/**
 * Keeps a story's media in step with the backend while it is being encoded.
 *
 * The upload answers before the bytes exist (KNOT-037-fix7), so a screen that shows
 * the story straight away has to be told when they arrive. This hook owns exactly
 * that: while the story has a `processing` item it re-reads the story every two
 * seconds, hands each fresh copy to `onUpdate`, and stops as soon as nothing is
 * processing any more — or after a minute, or when the screen unmounts
 * (KNOT-037-fix8).
 *
 * The loop itself is `runStoryPolling`, which takes its clock, its timer and its
 * fetcher as arguments, so the polling rules are tested without a renderer or real
 * timers; this hook is only the React plumbing around it.
 *
 * `onUpdate` must be stable (a `useCallback`): its identity is part of the effect's
 * dependencies, so an inline arrow would restart the poll on every render.
 */
export function useStoryPolling(
  story: Story | undefined,
  onUpdate: (story: Story) => void,
  token?: string,
): void {
  const storyId = story?.id;
  const processing = hasProcessingMedia(story?.media);

  useEffect(() => {
    if (storyId === undefined || !processing) {
      return;
    }

    let cancelled = false;

    void runStoryPolling({
      storyId,
      fetchStory: async (id: string): Promise<Story> => {
        const response = await storiesApi.getStory(id, token);
        return response.story;
      },
      onUpdate,
      // The composer uploads after it publishes, so "no media yet" is expected for
      // a while rather than a final answer (KNOT-037-fix12).
      awaitingMedia: true,
      isCancelled: () => cancelled,
    });

    return () => {
      cancelled = true;
    };
  }, [onUpdate, processing, storyId, token]);
}
