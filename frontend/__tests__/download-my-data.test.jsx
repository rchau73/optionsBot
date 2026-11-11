import { fireEvent, render, screen, within } from "@testing-library/react";
import DownloadMyDataModal, { MY_DATA_COPY } from "@/components/DownloadMyData";
import UserMenu from "@/components/UserMenu";
import { longSentences, undefinedJargon } from "@/test/plainWords";

// #750: Account ▾ → Download my data, its confirmation and the download.

function deps(over = {}) {
  return {
    downloadMyData: jest.fn(async () => ({ blob: new Blob(["{}"]), filename: "my-data-alice-2026-11-11.json" })),
    saveFile: jest.fn(),
    track: jest.fn(),
    ...over,
  };
}

describe("Download my data dialog", () => {
  test("the person's menu opens it, for any role, and closes the menu", () => {
    render(<UserMenu username="alice" role="user" />);
    fireEvent.click(screen.getByRole("button", { name: "alice ▾" }));
    fireEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name: "Download my data" }));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.getByRole("dialog", { name: "Download my data" })).toBeInTheDocument();
  });

  test("the confirmation says what the file holds, that it is theirs to keep safe and that it has no secret, in plain words", () => {
    const d = deps();
    render(<DownloadMyDataModal onClose={() => {}} deps={d} />);
    const dialog = screen.getByRole("dialog", { name: "Download my data" });
    for (const words of ["your account", "your survey answers", "your usage events", "your own audit lines", "Keep it somewhere safe", "no password hash", "no address"]) {
      expect(dialog).toHaveTextContent(words);
    }
    expect(d.downloadMyData).not.toHaveBeenCalled();
    for (const [key, text] of Object.entries(MY_DATA_COPY)) {
      expect([key, longSentences(text)]).toEqual([key, []]);
      expect([key, undefinedJargon(text, "my data")]).toEqual([key, []]);
    }
  });

  test("Scenario: A person downloads their data — the button saves the file and counts an ok download", async () => {
    const d = deps();
    render(<DownloadMyDataModal onClose={() => {}} deps={d} />);
    fireEvent.click(screen.getByRole("button", { name: MY_DATA_COPY.confirm }));
    expect(await screen.findByText(MY_DATA_COPY.done)).toBeInTheDocument();
    expect(d.saveFile).toHaveBeenCalledWith(expect.any(Blob), "my-data-alice-2026-11-11.json");
    expect(d.track).toHaveBeenCalledWith("my_data_downloaded", { outcome: "ok" });
    expect(screen.getByRole("button", { name: "Close window" })).toBeInTheDocument();
  });

  test("Scenario: Too many downloads — the server's reason is shown and a refused download counted", async () => {
    const refused = Object.assign(new Error("You can download your data 3 times an hour. Try again in 12 minutes."), { status: 429 });
    const d = deps({ downloadMyData: jest.fn(async () => Promise.reject(refused)) });
    render(<DownloadMyDataModal onClose={() => {}} deps={d} />);
    fireEvent.click(screen.getByRole("button", { name: MY_DATA_COPY.confirm }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Not downloaded: You can download your data 3 times an hour. Try again in 12 minutes.");
    expect(d.saveFile).not.toHaveBeenCalled();
    expect(d.track).toHaveBeenCalledWith("my_data_downloaded", { outcome: "refused" });
  });

  test("a failed download counts as an error; Cancel and Escape close without downloading", async () => {
    const d = deps({ downloadMyData: jest.fn(async () => Promise.reject(new Error("500"))) });
    const onClose = jest.fn();
    render(<DownloadMyDataModal onClose={onClose} deps={d} />);
    fireEvent.click(screen.getByRole("button", { name: MY_DATA_COPY.confirm }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Not downloaded: 500");
    expect(d.track).toHaveBeenCalledWith("my_data_downloaded", { outcome: "error" });
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
