import os, sys
for d, dirs, files in os.walk(sys.argv[1]):
    for n in sorted(dirs + files):
        p = os.path.join(d, n); st = os.lstat(p)
        # directories: mode only (their size/mtime change whenever an
        # entry is created and removed again; entries are compared
        # themselves); files and links: size, mtime, mode.
        d_ = os.path.isdir(p) and not os.path.islink(p)
        print("%s %d %d %o" % (os.path.relpath(p, sys.argv[1]), 0 if d_ else st.st_size, 0 if d_ else st.st_mtime_ns, st.st_mode))
