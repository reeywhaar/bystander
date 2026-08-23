import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  deleteAdminInvitesById,
  deleteAdminUsersById,
  getAdminInvites,
  getAdminUsers,
  patchAdminUsersById,
  postAdminInvites,
} from "@app/api/actions/admin";
import { getMe } from "@app/api/actions/auth";
import {
  deleteEditionItemsByIdRead,
  getEdition,
  postEditionRegenerate,
  putEditionItemsByIdRead,
} from "@app/api/actions/edition";
import {
  deleteFeedsById,
  getFeeds,
  patchFeedsById,
  postFeeds,
} from "@app/api/actions/feeds";
import { postFeedsDiscover } from "@app/api/actions/discover";
import {
  postFeedsExport,
  postFeedsImport,
  postFeedsImportPreview,
} from "@app/api/actions/opml";
import { getRead } from "@app/api/actions/read";
import { getSettings, patchSettings } from "@app/api/actions/settings";
import {
  deleteTagsById,
  getTags,
  patchTagsById,
  postTags,
} from "@app/api/actions/tags";
import { useApiCall } from "@app/api/provider";
import type { Article, Edition, ImportSelection, Role } from "@app/api/types";
import { qk } from "@app/queries/keys";

/**
 * One hook per thing this interface reads or writes.
 *
 * Declared here rather than at each `useQuery`, because the same read happens in several
 * places — the tag list is wanted by three components — and three declarations of one
 * query are three chances for them to disagree about the key, the request or the options.
 *
 * Every read threads TanStack's AbortSignal through to the request, so a superseded fetch
 * stops rather than racing the fetch that replaced it.
 */

export function useMe() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.me,
    queryFn: ({ signal }) => callApi(getMe(), signal),
    // Who you are does not change while you are looking at a page, and every island asks
    // on mount. Retrying a 401 would just be four requests to learn the same thing.
    retry: false,
  });
}

export function useEdition() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.edition,
    queryFn: ({ signal }) => callApi(getEdition(), signal),
  });
}

export function useRegenerate() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => callApi(postEditionRegenerate()),
    onSuccess: (page) => {
      // The response *is* the new page, so write it straight into the cache rather than
      // invalidating and asking for what we are already holding.
      client.setQueryData(qk.edition, page);
    },
  });
}

/**
 * Marks an article read or unread, optimistically.
 *
 * Optimistic because the whole gesture is "I have finished with this one", and a card that
 * waits for a round trip before greying makes that feel like a request rather than a
 * statement. On failure the previous page is put back, which is the only honest thing to
 * do with an optimistic update that did not happen.
 */
export function useSetRead() {
  const callApi = useApiCall();
  const client = useQueryClient();

  return useMutation({
    mutationFn: ({ id, read }: { id: string; read: boolean }) =>
      callApi(
        read ? putEditionItemsByIdRead(id) : deleteEditionItemsByIdRead(id),
      ),

    onMutate: async ({ id, read }) => {
      // Stop an in-flight refetch from landing on top of the optimistic write.
      await client.cancelQueries({ queryKey: qk.edition });
      const previous = client.getQueryData<Edition>(qk.edition);

      client.setQueryData<Edition>(qk.edition, (current) =>
        current
          ? {
              ...current,
              items: current.items.map((article: Article) =>
                article.id === id
                  ? {
                      ...article,
                      read_at: read ? Math.floor(Date.now() / 1000) : null,
                    }
                  : article,
              ),
            }
          : current,
      );
      return { previous };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) client.setQueryData(qk.edition, context.previous);
    },

    // The page is written optimistically above; the month-long record behind it is not,
    // because its ordering and its retention are the server's to decide.
    onSettled: () => {
      void client.invalidateQueries({ queryKey: qk.read });
    },
  });
}

export function useReadArticles() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.read,
    queryFn: ({ signal }) => callApi(getRead(), signal),
  });
}

export function useFeeds() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.feeds,
    queryFn: ({ signal }) => callApi(getFeeds(), signal),
  });
}

/**
 * Asks what a URL is. A mutation rather than a query because it makes the server fetch
 * somebody else's site — that is an action with a cost, not a cached read.
 */
export function useDiscoverFeeds() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: (url: string) => callApi(postFeedsDiscover(url)),
  });
}

export function useAddFeed() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: { url: string; tag_ids?: string[] }) =>
      callApi(postFeeds(input)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.feeds });
    },
  });
}

export function useUpdateFeed() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      changes,
    }: {
      id: string;
      changes: {
        priority?: number;
        title_override?: string;
        tag_ids?: string[];
        article_window?: number;
      };
    }) => callApi(patchFeedsById(id, changes)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.feeds });
    },
  });
}

export function useRemoveFeed() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => callApi(deleteFeedsById(id)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.feeds });
    },
  });
}

/** Builds a subscription list from the feeds that were ticked. */
export function useExportFeeds() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: (ids: string[]) => callApi(postFeedsExport(ids)),
  });
}

/** Reads a pasted list and says what it would do. Changes nothing. */
export function usePreviewImport() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: (opml: string) => callApi(postFeedsImportPreview(opml)),
  });
}

export function useImportFeeds() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (feeds: ImportSelection[]) => callApi(postFeedsImport(feeds)),
    onSuccess: () => {
      // An import creates tags as well as subscriptions, so both listings are stale.
      void client.invalidateQueries({ queryKey: qk.feeds });
      void client.invalidateQueries({ queryKey: qk.tags });
    },
  });
}

export function useTags() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.tags,
    queryFn: ({ signal }) => callApi(getTags(), signal),
  });
}

export function useAddTag() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      name: string;
      parent_id?: string;
      priority?: number;
    }) => callApi(postTags(input)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.tags });
    },
  });
}

export function useUpdateTag() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      changes,
    }: {
      id: string;
      changes: { name?: string; parent_id?: string; priority?: number };
    }) => callApi(patchTagsById(id, changes)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.tags });
    },
  });
}

export function useRemoveTag() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => callApi(deleteTagsById(id)),
    onSuccess: () => {
      // Deleting a tag detaches it from every subscription that carried it, so the feed
      // listing is stale too.
      void client.invalidateQueries({ queryKey: qk.tags });
      void client.invalidateQueries({ queryKey: qk.feeds });
    },
  });
}

export function useSettings() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.settings,
    queryFn: ({ signal }) => callApi(getSettings(), signal),
  });
}

export function useUpdateSettings() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (changes: {
      edition_interval?: number;
      edition_size?: number;
    }) => callApi(patchSettings(changes)),
    onSuccess: (settings) => {
      client.setQueryData(qk.settings, settings);
    },
  });
}

export function useUsers() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.adminUsers,
    queryFn: ({ signal }) => callApi(getAdminUsers(), signal),
  });
}

export function useSetUserDisabled() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, disabled }: { id: string; disabled: boolean }) =>
      callApi(patchAdminUsersById(id, { disabled })),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.adminUsers });
    },
  });
}

export function useRemoveUser() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => callApi(deleteAdminUsersById(id)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.adminUsers });
      // An account's invitation names who it became, so that listing changes too.
      void client.invalidateQueries({ queryKey: qk.adminInvites });
    },
  });
}

export function useInvites() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.adminInvites,
    queryFn: ({ signal }) => callApi(getAdminInvites(), signal),
  });
}

export function useCreateInvite() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (role: Role) => callApi(postAdminInvites(role)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.adminInvites });
    },
  });
}

export function useRemoveInvite() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => callApi(deleteAdminInvitesById(id)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.adminInvites });
    },
  });
}
