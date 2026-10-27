# cloudtop — working rules

## House rules

- No emoji, no hype, no toy UX. Plain copy, plain output.
- State uncertainty plainly rather than guessing. A value that could not be
  measured is `N/A`, never a zero.
- Say what was actually done. A claim in a commit message or a work package
  has to be true by the time it is written.
- Red, green, refactor. A test is written before the code it tests, and it
  has to be able to fail. A test that cannot fail is not a test.
- Every commit names its work package: `#1491` in the subject line.

## Conventions

- The `go` directive in `go.mod` is the **floor** the ports builder has, not
  the version on the developer's machine. Do not raise it casually.
- `CGO_ENABLED=0` everywhere: the NVIDIA path uses purego so the build needs
  no C compiler and no headers. The resulting binary is still dynamically
  linked, because reading the management library at run time needs libc.
- Every measured value carries the source that produced it. See
  `internal/model.Value`.
- Providers report what they can and leave the rest missing. A provider that
  cannot read a quantity must not invent one.
- A GPU is identified by PCI address plus UUID, never by enumeration index.
  The index changes when the machine boots with a different device order.

## Layout

```
cmd/cloudtop          entry point
internal/model        the data types every layer shares
internal/collect      kernel readers (/proc, /sys)
internal/gpu          GPU providers (sysfs, nvml)
```

Collectors, providers and the renderer meet at `internal/model`. Nothing
imports across those lines except the renderer, which reads snapshots.
