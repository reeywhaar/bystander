import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getSharesByToken, postShares } from "@app/api/actions/shares";
import {
  deleteAccountRecovery,
  getAccount,
  postAccountPassword,
  postAccountRecovery,
  postAccountRecoveryConfirm,
  putAccountPublicName,
} from "@app/api/actions/account";
import {
  deleteAdminInvitesById,
  deleteAdminUsersById,
  deleteAdminSmtp,
  getAdminInvites,
  getAdminSmtp,
  getAdminUsers,
  patchAdminUsersById,
  postAdminInvites,
  postAdminSmtpTest,
  putAdminSmtp,
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
  deleteFeedsByIdRead,
  getFeeds,
  patchFeedsById,
  postFeeds,
  postFeedsByIdRead,
  type MarkSpan,
} from "@app/api/actions/feeds";
import { postFeedsDiscover, postFeedsPreview } from "@app/api/actions/discover";
import {
  getImages,
  getImagesUnmeasured,
  postImagesRetry,
} from "@app/api/actions/images";
import { getInstance, putInstance } from "@app/api/actions/instance";
import {
  postFeedsExport,
  postFeedsImport,
  postFeedsImportPreview,
} from "@app/api/actions/opml";
import { getRead } from "@app/api/actions/read";
import {
  deleteTagsById,
  getTags,
  patchTagsById,
  postTags,
} from "@app/api/actions/tags";
import { useApiCall } from "@app/api/provider";
import type {
  InstanceSettings,
  Article,
  Edition,
  ImportSelection,
  Role,
  SmtpForm,
} from "@app/api/types";
import {
  deletePage,
  deletePagePublish,
  getPages,
  patchPage,
  postPage,
  putPagePublish,
  type PageChanges,
} from "@app/api/actions/pages";
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

/**
 * One page's live edition. Empty names the main page.
 *
 * Cached per page, so moving between tabs does not throw away the one being left — a reader
 * comes back to a front page expecting to find it where they left it, and re-fetching would
 * also mean re-drawing the seeded layout from scratch.
 */
/**
 * The page as it was when this tab loaded it, and it stays that way.
 *
 * Deliberately not polled, and not refetched on focus — see the defaults in mount.tsx. The
 * server composes a new edition when a page comes due, so a tab left open across a page turn
 * is showing an edition that is no longer the current one. That is the intended behaviour: a
 * page arriving under somebody mid-read would rearrange what they are looking at and take
 * their read marks with it, since a read mark belongs to the composition it was made on. The
 * whole premise of this reader is that nothing on the page moves.
 *
 * So a page turns when it is next loaded. Reloading is the gesture, and it is the reader's.
 *
 * Regenerating is the exception and not a counter-example: the response *is* the new page and
 * it is written straight into the cache — but somebody asked for it, which is the difference.
 */
export function useEdition(page = "") {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.editionOf(page),
    queryFn: ({ signal }) => callApi(getEdition(page), signal),
  });
}

export function useRegenerate(page = "") {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => callApi(postEditionRegenerate(page)),
    onSuccess: (edition) => {
      // The response *is* the new page, so write it straight into the cache rather than
      // invalidating and asking for what we are already holding.
      client.setQueryData(qk.editionOf(page), edition);
      // Composing moves that page's clock, and the strip shows when each is next due.
      void client.invalidateQueries({ queryKey: qk.pages });
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
      // Every page held, not just the one being looked at.
      //
      // Reading is a fact about a person and an article, and the server marks it on every page
      // the article is on. An optimistic update that touched only the visible page would
      // disagree with the server the moment somebody switched tabs — and would then be
      // silently corrected on the next fetch, which is the confusing way round.
      const previous = client.getQueriesData<Edition>({ queryKey: qk.edition });

      client.setQueriesData<Edition>({ queryKey: qk.edition }, (current) =>
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
      for (const [key, edition] of context?.previous ?? []) {
        client.setQueryData(key, edition);
      }
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

/**
 * Asks what a feed has published, without following it.
 *
 * A mutation for the same reason discovery is one: it makes the server fetch somebody else's
 * site, which is an action with a cost rather than a cached read. It also means pressing
 * Preview a second time asks again, which is what somebody who left the dialog open for a
 * while would expect.
 */
export function usePreviewFeed() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: (url: string) => callApi(postFeedsPreview(url)),
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
/**
 * Turns a selection into a link.
 *
 * A mutation, not a query: it writes a row and hands back a token that exists exactly once
 * in the answer. A query that refetched on focus would mint links nobody asked for.
 */
export function useCreateShare() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: (ids: string[]) => callApi(postShares(ids)),
  });
}

/** What a link holds. Opening one changes nothing. */
export function useSharedList(token: string) {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.share(token),
    queryFn: ({ signal }) => callApi(getSharesByToken(token), signal),
    // A link is a snapshot and expiry is checked on the server, so there is nothing here
    // that goes stale while somebody is looking at it.
    staleTime: Infinity,
    retry: false,
  });
}

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

/**
 * Marks a feed's articles read, as far back as was asked for.
 *
 * Everything is invalidated afterwards rather than patched: this touches articles on every
 * page, the record behind Recently read, and the feed row itself, and working out which of
 * those changed would be re-deriving on the client what the server already decided.
 */
export function useMarkFeedRead() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, olderThan }: { id: string; olderThan: MarkSpan }) =>
      callApi(postFeedsByIdRead(id, olderThan)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.edition });
      void client.invalidateQueries({ queryKey: qk.read });
      void client.invalidateQueries({ queryKey: qk.feeds });
    },
  });
}

/** Forgets that anything from a feed was read. The inverse of {@link useMarkFeedRead}. */
export function useUnmarkFeedRead() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => callApi(deleteFeedsByIdRead(id)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.edition });
      void client.invalidateQueries({ queryKey: qk.read });
      void client.invalidateQueries({ queryKey: qk.feeds });
    },
  });
}

/** Every page this person has, main first — the tab strip. */
export function usePages() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.pages,
    queryFn: ({ signal }) => callApi(getPages(), signal),
  });
}

export function useCreatePage() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (page: { name: string; slug: string }) =>
      callApi(postPage(page)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.pages });
    },
  });
}

/**
 * Saves a page.
 *
 * One mutation for every control on a page, whether it came from the dialog's single save or
 * from a cadence button pressed on its own. The distinction that matters is in the body: a
 * field left out is left alone, so a button that changes the size sends only the size.
 */
export function useUpdatePage() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, changes }: { id: string; changes: PageChanges }) =>
      callApi(patchPage(id, changes)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.pages });
      // A page that draws from different things now shows different things, and its address
      // may have moved. Both make every held edition stale.
      void client.invalidateQueries({ queryKey: qk.edition });
    },
  });
}

export function useDeletePage() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => callApi(deletePage(id)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.pages });
      void client.invalidateQueries({ queryKey: qk.edition });
    },
  });
}

export function useAccount() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.account,
    queryFn: ({ signal }) => callApi(getAccount(), signal),
  });
}

/** Sends a code. Nothing about the account changes until it comes back. */
export function useBeginRecovery() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (email: string) => callApi(postAccountRecovery(email)),
    onSuccess: () => {
      // Only the pending address moved, and the answer carries nothing, so this is the one
      // that has to go back and ask.
      void client.invalidateQueries({ queryKey: qk.account });
    },
  });
}

export function useConfirmRecovery() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => callApi(postAccountRecoveryConfirm(code)),
    onSuccess: (account) => {
      // The response is the account as stored, so it replaces the cache outright rather
      // than prompting a second request for what we were just handed.
      client.setQueryData(qk.account, account);
    },
  });
}

export function useForgetRecovery() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => callApi(deleteAccountRecovery()),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.account });
    },
  });
}

/**
 * Changes your password.
 *
 * Nothing is invalidated afterwards: this session survives on purpose, and none of what is
 * cached describes a password.
 */
export function useChangePassword() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: (passwords: {
      current_password: string;
      new_password: string;
    }) => callApi(postAccountPassword(passwords)),
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
    mutationFn: ({ role, email }: { role: Role; email?: string }) =>
      callApi(postAdminInvites(role, email ?? "")),
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

export function useSmtp() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.adminSmtp,
    queryFn: ({ signal }) => callApi(getAdminSmtp(), signal),
  });
}

export function useSaveSmtp() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (config: SmtpForm) => callApi(putAdminSmtp(config)),
    onSuccess: (saved) => {
      // The response is the configuration as stored, so it replaces the cache outright
      // rather than prompting a second request for what we were just handed.
      client.setQueryData(qk.adminSmtp, saved);
    },
  });
}

export function useForgetSmtp() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => callApi(deleteAdminSmtp()),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: qk.adminSmtp });
    },
  });
}

/**
 * Sends one real message.
 *
 * Deliberately not a query: it has an effect out in the world, and a query that retries or
 * refetches on focus would put mail in somebody's inbox for looking at a browser tab.
 */
export function useTestSmtp() {
  const callApi = useApiCall();
  return useMutation({
    mutationFn: ({ to, relay }: { to: string; relay?: SmtpForm }) =>
      callApi(postAdminSmtpTest(to, relay)),
  });
}

/** How the pictures on this instance are getting on. Administrators only. */
export function useImages() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.adminImages,
    queryFn: ({ signal }) => callApi(getImages(), signal),
  });
}

/**
 * The pictures behind one of the counts on the images screen. Administrators only.
 *
 * `enabled` rather than a conditional hook, because this is asked for by a dialog: the
 * component is mounted with the reason it was opened on and the query runs then, not while the
 * row behind it is merely being looked at.
 */
export function useUnmeasuredImages(reason: string, enabled: boolean) {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.adminImagesUnmeasured(reason),
    queryFn: ({ signal }) => callApi(getImagesUnmeasured(reason), signal),
    enabled,
  });
}

/**
 * Offers unmeasured pictures back to the measuring queue — one, a category, or all of them.
 *
 * The count that comes back is a promise rather than a result: the queue takes them one every
 * few seconds, which is the politeness the whole thing is arranged around. So the tally is
 * invalidated to show the new state of play, and it will keep moving afterwards on its own.
 *
 * Every list under it is invalidated too, and by prefix rather than by the one reason that was
 * reset: a picture that has been offered again leaves the group it was in, so the group it was
 * in is not the only list that changed.
 */
export function useRetryImages() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (what: { reason?: string; url?: string }) =>
      callApi(postImagesRetry(what)),
    onSuccess: () =>
      void client.invalidateQueries({ queryKey: qk.adminImages }),
  });
}

/**
 * Sets the name this account's published pages live under, or gives it up.
 *
 * The response is the account read fresh, so it is written straight into the cache rather than
 * invalidated and asked for again — the screen never has a moment where the write has landed
 * and the page still says otherwise.
 */
export function useSetPublicName() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => callApi(putAccountPublicName(name)),
    onSuccess: (account) => client.setQueryData(qk.account, account),
  });
}

/** What this instance serves to strangers. Administrators only. */
export function useInstance() {
  const callApi = useApiCall();
  return useQuery({
    queryKey: qk.adminInstance,
    queryFn: ({ signal }) => callApi(getInstance(), signal),
  });
}

export function useSetInstance() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (settings: InstanceSettings) => callApi(putInstance(settings)),
    onSuccess: (saved) => client.setQueryData(qk.adminInstance, saved),
  });
}

/**
 * Puts a page on the open web, or takes it down.
 *
 * One hook for both, because they are one decision with two answers and the screen shows one
 * button that changes its mind. The pages list is invalidated rather than patched: publishing
 * is the sort of thing somebody wants to see reflected everywhere at once.
 */
export function usePublishPage() {
  const callApi = useApiCall();
  const client = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; slug?: string; indexable?: boolean }) =>
      input.slug === undefined
        ? callApi(deletePagePublish(input.id))
        : callApi(
            putPagePublish(input.id, input.slug, input.indexable ?? false),
          ),
    onSuccess: () => void client.invalidateQueries({ queryKey: qk.pages }),
  });
}
