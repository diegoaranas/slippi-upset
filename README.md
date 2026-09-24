# slippi-upset

Melee announcer callouts for Slippi netplay. Run it in the background while you
play, and it reacts to who you're playing and whether you beat them:

| When | Sound |
|---|---|
| Game 1 starts against someone rated higher than you | *Challenger Approaching* jingle |
| You beat the highest-rated opponent you've ever beaten | "A new record!" |
| You beat someone whose best season beats your best season | "Wow! Incredible!" |
| You beat someone whose current rating beats yours | "Congratulations!" |
| Any other win | "Complete!" |
| Your opponent quits (resets) mid-game | "No contest!" |

Only the first matching win sound plays. Losses, your own quits and doubles are silent.
Ratings come from slippi.gg, so it works for ranked and unranked games alike.

## Download

1. Download [`upset_windows_amd64.exe`](https://github.com/diegoaranas/slippi-upset/releases/latest/download/upset_windows_amd64.exe)
   (Windows), or the `upset_linux_*` / `upset_darwin_*` archive for your system from the
   [latest release](https://github.com/diegoaranas/slippi-upset/releases/latest).
2. Put it in its own folder. It saves its sounds and your record next to itself.
3. Run it before you play (double-click on Windows, `./upset` elsewhere) and leave the window open.

You need Windows, Linux or macOS and Slippi Launcher (logged in). Your connect
code and replay folder are detected automatically.

On Linux, sounds play through `pw-play`, `paplay` or `aplay`, whichever
is installed; on macOS through `afplay`.

macOS blocks downloaded apps that aren't signed. After extracting, run
`xattr -d com.apple.quarantine upset` once to allow it.

On first run it downloads the announcer clips. They're Nintendo's, so they aren't
included here; it fetches the community rips from [The Sounds Resource](https://sounds.spriters-resource.com/gamecube/ssbm/).

Windows may warn that the app is from an unknown publisher, because it isn't
code-signed. Click **More info → Run anyway**, or build it yourself (below).

## Build from source

Requires **Go 1.26+**. The only dependency is [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys/windows) for Windows system calls.

```
git clone https://github.com/diegoaranas/slippi-upset
cd slippi-upset
go build .
```

That builds `upset.exe` on Windows or `upset` on Linux and macOS. To build the Windows .exe
from another system: `GOOS=windows GOARCH=amd64 go build .`

The first run downloads the clips to `sounds/` at half volume. To re-download
them at full volume: `upset --get-sounds --volume 1`.

```
Watching C:\Users\you\Documents\Slippi for new games as ABCD#123... (Ctrl+C to stop)
New opponent: EFGH#456  2210.4 (peak 2301.7)  >>> CHALLENGER APPROACHING
Game_20260923T221106.slp: WIN vs EFGH#456  them 2210.4 (peak 2301.7)  you 2146.6 (peak 2146.6)  >>> NEW RECORD! (was 1950.3)
```

To check it on a game you've already played:

```
upset --test "C:\path\to\Game_20260923T221106.slp"
```

## Configuration

Everything is at the top of `main.go` (rebuild after changing it):

- `myCode` / `replayDir`: detected from Slippi Launcher. Set them only if detection fails or you keep replays somewhere unusual.
- `sound*`: any `.wav` file works. `soundConnect` plays for every *other* new opponent; it's off by default (try `sounds/versus.wav`).

Your record is kept in `record.json` next to the program. It starts at 0, so
your first win sets it. Delete the file to reset.

## How it works

Slippi writes a replay file as each game is played. It checks the
replay folder once a second:

- **Game starts:** reads the first 2 KB of the new file to get the connect codes.
- **Game ends:** Slippi fills in the replay's length header. It then reads the replay once to find the winner and looks up both players' ratings.

It makes no network requests during a game and runs at below-normal CPU
priority, so it doesn't affect Dolphin.

Ratings come from the same API the slippi.gg profile pages use. It's not an
official public API, so it could change without notice.

## License

MIT for the code in this repo. The announcer clips belong to Nintendo and
aren't covered by this license.
