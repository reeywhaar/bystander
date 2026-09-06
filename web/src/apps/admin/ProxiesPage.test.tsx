import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Proxy } from "@app/api/types";
import { renderWith } from "@app/test/harness";

import { ProxiesPage } from "@app/apps/admin/ProxiesPage";

const frankfurt: Proxy = {
  id: "px_1",
  kind: "proxio",
  label: "Frankfurt",
  url: "http://proxio.internal:80",
  username: "",
  priority: 100,
  enabled: true,
  has_token: true,
  routes: 2,
  created_at: Math.floor(Date.now() / 1000) - 86400,
  updated_at: Math.floor(Date.now() / 1000) - 3600,
};

const tunnel: Proxy = {
  ...frankfurt,
  id: "px_2",
  kind: "socks5",
  label: "",
  url: "socks5://tunnel.example.com:1080",
  username: "operator",
  priority: 20,
  enabled: false,
  routes: 0,
};

const render = (proxies: Proxy[], overrides: Record<string, unknown> = {}) =>
  renderWith(<ProxiesPage />, {
    "GET /api/admin/proxies": { body: { proxies } },
    "POST /api/admin/proxies": { body: frankfurt, status: 201 },
    "PUT /api/admin/proxies/px_1": { body: frankfurt },
    "PUT /api/admin/proxies/px_2": { body: tunnel },
    "DELETE /api/admin/proxies/px_1": { status: 204 },
    "POST /api/admin/proxies/test": { body: { ok: true, status: 200 } },
    ...overrides,
  });

const dialog = () => screen.getByRole("dialog");

/** One row, once the list has arrived. Narrows the checked index in one place. */
const rowAt = async (n: number) => {
  const found = await screen.findAllByRole("listitem");
  const row = found[n];
  if (!row) throw new Error(`there is no relay at row ${n}`);
  return row;
};

/** The first row, which is the one most assertions are about. */
const firstRow = () => rowAt(0);

describe("ProxiesPage", () => {
  it("says nothing is configured rather than showing an empty table", async () => {
    render([]);
    expect(
      await screen.findByText(/every publisher is reached directly/i),
    ).toBeInTheDocument();
  });

  it("says what to go and get when there is nothing configured", async () => {
    render([]);
    // Someone here has decided they might want one; the next question is what to install.
    const link = await screen.findByRole("link", { name: /proxio/i });
    expect(link).toHaveAttribute("href", "https://github.com/reeywhaar/proxio");
    expect(screen.getByText(/socks5 endpoint you already have/i)).toBeInTheDocument();
  });

  it("says what proxio is, with somewhere to get it, and what SOCKS5 needs instead", async () => {
    render([]);
    await userEvent.click(await screen.findByRole("button", { name: /add a relay/i }));

    const link = within(dialog()).getByRole("link", { name: /proxio/i });
    expect(link).toHaveAttribute("href", "https://github.com/reeywhaar/proxio");
    expect(
      within(dialog()).getByText(/a small http relay you run yourself/i),
    ).toBeInTheDocument();

    await userEvent.selectOptions(
      within(dialog()).getByLabelText(/kind/i),
      "socks5",
    );
    // A standard, so there is nothing to link to and nothing to install.
    expect(
      within(dialog()).queryByRole("link", { name: /proxio/i }),
    ).not.toBeInTheDocument();
    expect(
      within(dialog()).getByText(/nothing to install here/i),
    ).toBeInTheDocument();
  });

  it("shows an address somebody could copy, and never one on this network", async () => {
    render([]);
    await userEvent.click(await screen.findByRole("button", { name: /add a relay/i }));

    expect(within(dialog()).getByLabelText(/address/i)).toHaveAttribute(
      "placeholder",
      "https://proxio.example.com",
    );
    // A relay beside this one goes out from this address, so a publisher refusing us refuses
    // it the same way. Suggesting the compose service name would point at the one deployment
    // guaranteed not to help.
    expect(within(dialog()).queryByText(/proxio:80/)).not.toBeInTheDocument();
    expect(
      within(dialog()).getByText(/somewhere this instance is not/i),
    ).toBeInTheDocument();

    await userEvent.selectOptions(
      within(dialog()).getByLabelText(/kind/i),
      "socks5",
    );
    expect(within(dialog()).getByLabelText(/address/i)).toHaveAttribute(
      "placeholder",
      "socks5://socks.example.com:1080",
    );
  });

  it("shows each relay's kind and address, and which one is switched off", async () => {
    render([frankfurt, tunnel]);

    const first = await rowAt(0);
    const second = await rowAt(1);
    expect(within(first).getByText(/Frankfurt/)).toBeInTheDocument();
    expect(within(first).getByText(/proxio/)).toBeInTheDocument();
    expect(within(first).getByText(/proxio\.internal:80/)).toBeInTheDocument();

    // No label, so the address stands in as the name — and is not then repeated below it.
    expect(
      within(second).getAllByText(/socks5:\/\/tunnel\.example\.com:1080/),
    ).toHaveLength(1);
    expect(within(second).getByText(/SOCKS5/)).toBeInTheDocument();
    // Its username belongs on screen; its password does not exist as far as this page knows.
    expect(within(second).getByText(/operator/)).toBeInTheDocument();
    expect(within(second).getByText(/off/)).toBeInTheDocument();
  });

  it("never puts a token on screen, because it was never sent one", async () => {
    render([frankfurt, tunnel]);
    await screen.findAllByRole("listitem");
    expect(screen.queryByDisplayValue(/token/i)).not.toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/has_token/);
  });

  it("asks the kind first, and asks for a username only where one is used", async () => {
    render([]);
    await userEvent.click(await screen.findByRole("button", { name: /add a relay/i }));

    // proxio takes one opaque token and no name.
    expect(within(dialog()).getByLabelText(/token/i)).toBeInTheDocument();
    expect(
      within(dialog()).queryByLabelText(/username/i),
    ).not.toBeInTheDocument();

    await userEvent.selectOptions(
      within(dialog()).getByLabelText(/kind/i),
      "socks5",
    );

    // SOCKS5 takes a name and calls the secret a password, because that is what it is.
    expect(within(dialog()).getByLabelText(/username/i)).toBeInTheDocument();
    expect(within(dialog()).getByLabelText(/password/i)).toBeInTheDocument();
  });

  it("will not save a relay without the parts its kind needs", async () => {
    render([]);
    await userEvent.click(await screen.findByRole("button", { name: /add a relay/i }));

    const save = within(dialog()).getByRole("button", { name: /^save$/i });
    expect(save).toBeDisabled();

    await userEvent.type(
      within(dialog()).getByLabelText(/address/i),
      "http://proxio:80",
    );
    expect(save).toBeDisabled();

    await userEvent.type(within(dialog()).getByLabelText(/token/i), "sekrit");
    expect(save).toBeEnabled();
  });

  it("can try a relay that has not been saved yet", async () => {
    render([]);
    await userEvent.click(await screen.findByRole("button", { name: /add a relay/i }));

    // Nothing typed yet, so there is nothing to dial.
    expect(within(dialog()).getByRole("button", { name: /try it/i })).toBeDisabled();

    await userEvent.type(
      within(dialog()).getByLabelText(/address/i),
      "https://proxio.example.com",
    );
    await userEvent.type(within(dialog()).getByLabelText(/token/i), "sekrit");

    // And now it can be tried, without having been saved over anything.
    await userEvent.click(within(dialog()).getByRole("button", { name: /try it/i }));
    const dialogs = await screen.findAllByRole("dialog");
    const test = dialogs[dialogs.length - 1]!;
    expect(within(test).getByLabelText(/address to fetch/i)).toBeInTheDocument();
  });

  it("leaves a stored token alone when the field is left empty", async () => {
    render([frankfurt]);
    const row = await firstRow();
    await userEvent.click(within(row).getByRole("button", { name: /change/i }));

    // Nothing is prefilled, and it says why rather than looking like an empty password.
    const token = within(dialog()).getByLabelText(/token/i);
    expect(token).toHaveValue("");
    expect(
      within(dialog()).getByText(/leave this empty to keep the one already set/i),
    ).toBeInTheDocument();

    // Saving without typing one is allowed, because empty means "the one already there".
    expect(within(dialog()).getByRole("button", { name: /^save$/i })).toBeEnabled();
  });

  it("asks what to fetch, and says what came back", async () => {
    render([frankfurt], {
      "POST /api/admin/proxies/test": {
        body: { ok: true, status: 200, url: "https://www.example.com/feed" },
      },
    });
    const row = await firstRow();
    await userEvent.click(within(row).getByRole("button", { name: /try it/i }));

    // The useful test is "does this get me past the thing that refused us", so it asks.
    const target = within(dialog()).getByLabelText(/address to fetch/i);
    await userEvent.type(target, "https://www.example.com/feed");
    await userEvent.click(
      within(dialog()).getByRole("button", { name: /^try it$/i }),
    );

    expect(await screen.findByText(/it got through/i)).toBeInTheDocument();
    expect(
      screen.getByText(/https:\/\/www\.example\.com\/feed answered 200/i),
    ).toBeInTheDocument();
  });

  it("reports a relay's own refusal as the relay's, not as the target's", async () => {
    render([frankfurt], {
      "POST /api/admin/proxies/test": {
        body: {
          ok: false,
          error: "Frankfurt answered 401 Unauthorized and said the fault was its own",
        },
      },
    });
    const row = await firstRow();
    await userEvent.click(within(row).getByRole("button", { name: /try it/i }));
    await userEvent.click(
      within(dialog()).getByRole("button", { name: /^try it$/i }),
    );

    expect(await screen.findByText(/the fault was its own/i)).toBeInTheDocument();
  });

  it("says what each relay is carrying, so resetting it is not a guess", async () => {
    render([frankfurt, tunnel]);

    const first = await rowAt(0);
    expect(
      within(first).getByText(/2 publishers reached through it/i),
    ).toBeInTheDocument();
    expect(within(first).getByRole("button", { name: /reset/i })).toBeInTheDocument();

    // Nothing learned through it, so there is nothing to forget and no button to press.
    const second = await rowAt(1);
    expect(within(second).queryByText(/reached through it/i)).not.toBeInTheDocument();
    expect(
      within(second).queryByRole("button", { name: /reset/i }),
    ).not.toBeInTheDocument();
  });

  it("forgets what one relay learned without removing the relay", async () => {
    render([frankfurt], {
      "POST /api/admin/proxies/px_1/reset": { body: { forgotten: 2 } },
    });
    const row = await firstRow();
    await userEvent.click(within(row).getByRole("button", { name: /reset/i }));

    expect(await screen.findByText(/forgotten\./i)).toBeInTheDocument();
    // The relay is still there — this is about what was learned through it.
    expect(await rowAt(0)).toBeInTheDocument();
  });

  it("says how much a delete would take with it", async () => {
    render([frankfurt]);
    const row = await firstRow();
    await userEvent.click(within(row).getByRole("button", { name: /delete/i }));

    expect(
      within(dialog()).getByText(/2 publishers reached through it go back/i),
    ).toBeInTheDocument();
  });

  it("orders relays by priority, highest first, and says so", async () => {
    render([]);
    await userEvent.click(await screen.findByRole("button", { name: /add a relay/i }));

    // A relay somebody has just gone to the trouble of configuring is one they want used.
    expect(within(dialog()).getByLabelText(/priority/i)).toHaveValue("100");
    expect(
      within(dialog()).getByText(/higher is tried first/i),
    ).toBeInTheDocument();
    // The one thing the number does not mean, said before somebody assumes it does.
    expect(
      within(dialog()).getByText(/zero means tried last rather than never/i),
    ).toBeInTheDocument();
  });

  it("explains that deleting loses the credential and switching off does not", async () => {
    render([frankfurt]);
    const row = await firstRow();
    await userEvent.click(within(row).getByRole("button", { name: /delete/i }));

    expect(
      within(dialog()).getByText(/switch it off instead/i),
    ).toBeInTheDocument();

    await userEvent.click(
      within(dialog()).getByRole("button", { name: /delete it/i }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
