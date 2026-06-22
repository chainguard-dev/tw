# `syspeek`

The `syspeek` tool statically analyses an ELF binary by disassembling and reporting a syscall profile.

The syscall profile can then be compared to a one dynamically generated when running functional tests
for the same application executable.

The only application type supported are ones compiled. Script and application that use interpreted languages
are not supported by this method.

## Requirements

Runtime requirements:

- binutils (`objdump` tool)
- syscall table file (`/usr/include/asm/unistd_64.h` by default)
- `objdump` compiled for the same architecture of the target executable

```shell
syspeek EXECUTABLE
```

## Quickstart

```shell
$ syspeek myapp
openat
read
gettid
getpid
gettid
tgkill
getpid
kill
getpid
tgkill
setitimer
timer_create
timer_settime
timer_delete
mincore
clock_gettime
rt_sigprocmask
rt_sigaction
mmap
munmap
madvise
futex
clone
gettid
exit
sigaltstack
arch_prctl
sched_yield
sched_getaffinity
clock_gettime
```

## Limitations

There are natural limitations on the static analysis this command does of syscall parameters,
due to the nature of the stack and the architecture-specific calling conventions.

Furthermore, some language compilers embeds the runtime into the binary, like Go does.
Consequently it requires to filter out runtime's sycalls.
