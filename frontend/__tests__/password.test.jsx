import { fireEvent, render, screen, within } from "@testing-library/react";
import ChangePasswordModal, { formProblem } from "@/components/ChangePassword";
import UserMenu from "@/components/UserMenu";

const reply = (status, body = {}) => ({ ok: status < 300, status, headers: new Headers(), json: async () => body });

function fill({ current = "my current password", next = "a brand new password", confirm = next } = {}) {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: current } });
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: next } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: confirm } });
  fireEvent.click(screen.getByRole("button", { name: "Change password" }));
}

describe("Change password form", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("the header's user menu opens it", () => {
    render(<UserMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Account ▾" }));
    const menu = screen.getByRole("menu");
    expect(within(menu).getByRole("menuitem", { name: "Sign out" })).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Change password" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("At least 12 characters");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  test("shows the 12-character rule and refuses a short or mismatched password before sending", () => {
    global.fetch = jest.fn();
    render(<ChangePasswordModal onClose={() => {}} />);
    expect(screen.getByText(/At least 12 characters/)).toBeInTheDocument();
    fill({ next: "short pass" });
    expect(screen.getByRole("alert")).toHaveTextContent("Too short: at least 12 characters");
    fill({ next: "a brand new password", confirm: "another new password" });
    expect(screen.getByRole("alert")).toHaveTextContent("The two new passwords do not match");
    expect(global.fetch).not.toHaveBeenCalled();
  });

  test("a wrong current password shows the server's plain words and clears that field", async () => {
    global.fetch = jest.fn(async () => reply(401, { error: "Current password is wrong" }));
    render(<ChangePasswordModal onClose={() => {}} />);
    fill();
    expect(await screen.findByRole("alert")).toHaveTextContent("Current password is wrong");
    expect(screen.getByLabelText("Current password")).toHaveValue("");
    const [url, init] = global.fetch.mock.calls[0];
    expect(url).toBe("/api/auth/password");
    expect(JSON.parse(init.body)).toEqual({ current: "my current password", next: "a brand new password" });
    expect(global.fetch).toHaveBeenCalledTimes(1); // no refresh: not a session 401
  });

  test("success says this browser stays signed in", async () => {
    global.fetch = jest.fn(async () => reply(200, { user: { username: "ana", role: "user" } }));
    const onClose = jest.fn();
    render(<ChangePasswordModal onClose={onClose} />);
    fill();
    expect(await screen.findByRole("status")).toHaveTextContent("You stay signed in here");
    fireEvent.click(screen.getByRole("button", { name: "Close window" }));
    expect(onClose).toHaveBeenCalled();
  });

  test("formProblem", () => {
    const ok = { current: "x", next: "y".repeat(12), confirm: "y".repeat(12) };
    expect(formProblem(ok)).toBeNull();
    expect(formProblem({ ...ok, current: "" })).toBe("Enter your current password");
    expect(formProblem({ ...ok, current: ok.next })).toBe("The new password must differ from the current one");
  });
});
