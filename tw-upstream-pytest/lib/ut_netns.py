import errno, fcntl, os, socket, struct, sys
CLONE_NEWNET, SIOCGIFFLAGS, SIOCSIFFLAGS, IFF_UP = 0x40000000, 0x8913, 0x8914, 0x1

def unshare_net():
    if hasattr(os, "unshare"):  # 3.12+
        try:
            os.unshare(CLONE_NEWNET)
        except OSError as e:
            raise OSError(e.errno, "unshare(CLONE_NEWNET): " + (e.strerror or ""))
        return
    import ctypes
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.unshare(CLONE_NEWNET) != 0:
        e = ctypes.get_errno()
        raise OSError(e, "unshare(CLONE_NEWNET): " + os.strerror(e))

def lo_up():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        flags = struct.unpack("16sH22x", fcntl.ioctl(s, SIOCGIFFLAGS, struct.pack("16sH22x", b"lo", 0)))[1]
        fcntl.ioctl(s, SIOCSIFFLAGS, struct.pack("16sH22x", b"lo", flags | IFF_UP))
    finally:
        s.close()

def v6_loopback():
    try:
        with socket.socket(socket.AF_INET6) as s:
            s.bind(("::1", 0))
        return True
    except OSError:
        return False

def check(v6):
    names = sorted(n for _, n in socket.if_nameindex())
    if names != ["lo"]:
        raise RuntimeError("interfaces in the new namespace: %s" % " ".join(names))
    for fam, addr in [(socket.AF_INET, "127.0.0.1")] + ([(socket.AF_INET6, "::1")] if v6 else []):
        with socket.socket(fam) as srv:
            srv.bind((addr, 0))
            srv.listen(1)
            with socket.create_connection(srv.getsockname()[:2], timeout=5) as c:
                a, _ = srv.accept()
                c.sendall(b"x")
                ok = a.recv(1) == b"x"
                a.close()
            if not ok:
                raise RuntimeError("no loopback round trip on %s" % addr)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as u:
        try:
            u.connect(("192.0.2.1", 9))  # TEST-NET-1: must have no route
        except OSError as e:
            if e.errno != errno.ENETUNREACH:
                raise
        else:
            raise RuntimeError("192.0.2.1 is routable inside the namespace")

mode = sys.argv[1]
try:
    v6 = v6_loopback()
    unshare_net()
    lo_up()
    check(v6)
except Exception as e:
    if mode == "probe":
        print(" ".join(str(e).split()) or type(e).__name__)
        sys.exit(1)
    sys.stderr.write("upstream-tests: cannot enter an isolated network namespace: %s\n" % e)
    sys.exit(70)
if mode == "probe":
    sys.exit(0)
os.execvp(sys.argv[2], sys.argv[2:])
