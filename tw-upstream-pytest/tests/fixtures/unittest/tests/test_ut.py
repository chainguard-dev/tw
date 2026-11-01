import unittest

import demo


class DemoTest(unittest.TestCase):
    def test_add(self):
        self.assertEqual(demo.add(1, 2), 3)
