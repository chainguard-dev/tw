import demo


def test_plugin_disabled(request):
    assert not request.config.pluginmanager.has_plugin("asyncio")


def test_double_sync():
    import asyncio
    assert asyncio.run(demo.double(21)) == 42
