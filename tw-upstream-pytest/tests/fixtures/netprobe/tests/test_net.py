# Negative control (py3-six.netprobe): a test that needs the network.
import socket

import demo


def test_offline_ok():
    assert demo.add(1, 1) == 2


def test_needs_network():
    s = socket.create_connection(("example.com", 80), timeout=10)
    s.close()


def test_needs_network_by_ip():
    s = socket.create_connection(("93.184.215.14", 80), timeout=10)
    s.close()
