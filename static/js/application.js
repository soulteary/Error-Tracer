;
(function ($) {
    $(document).ready(function () {

        var cacheTime = 1000 * 60 * 100;
        100 //100min;

        var loader = function (mode) {
            $('.progress .bar').attr('class', 'bar');
            if (mode == 'show') {
                $('.progress').fadeIn().find('.bar').css('width', '100%');
            } else if (mode == 'hide') {
                $('.progress').find('.bar').addClass('bar-success').closest('.progress').fadeOut().find('.bar').css('width', 0);
            } else if (mode == 'suspend') {
                $('.progress').find('.bar').addClass('bar-warning');
            }
        }

        var count = function (data) {
            var mapReduce = {
                map: function (object) {
                    return {key: object['hash'], value: 1};
                },
                reduce: function (allSteps) {
                    var result = {};
                    for (var i = 0; i < allSteps.length; i++) {
                        var step = allSteps[i];
                        result[step.key] = result[step.key] ? (result[step.key] + 1) : 1;
                    }
                    return result;
                },
                init: function () {
                    var allSteps = [];
                    for (var i = 0; i < data.length; i++) {
                        allSteps = allSteps.concat(mapReduce.map(data[i]));
                    }
                    var tmp = mapReduce.reduce(allSteps);
                    var result = [];
                    for (var oo in tmp) {
                        result[oo] = [];
                        result[oo]['count'] = tmp[oo];
                    }
                    return result;
                }
            }
            return mapReduce.init();
        }

        var RandomChars = function (strlen, opt, cut) {
            var chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz1234567890";
            if (opt) {
                switch (opt) {
                    case 'NUMBER':
                        chars = "1234567890";
                        break;
                    case 'UCASE':
                        chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
                        break;
                    case 'LCASE':
                        chars = "abcdefghijklmnopqrstuvwxyz";
                        break;
                }
            }
            if (cut) {
                for (var i = cut.length - 1; i >= 0; i--) {
                    chars = chars.replace(cut[i], '');
                }
            }

            var len = chars.length;
            var result = "";
            if (!strlen) {
                strlen = Math.random(len);
            }
            var d = Date.parse(new Date());
            for (var i = 0; i < strlen; i++) {
                result += chars.charAt(Math.ceil(Math.random() * d) % len);
            }
            return  result;
        }

        var initData = function (params) {
            var url = params.url || '?mode=admin&a=query';
            var cb = params.callback || null;
            var mode = params.mode || 'MSG';

            var initMSGTable = function (data) {
                loader('hide');
                $('#data-table tbody').empty();
                var html = '';
                if (!data['data'] && data['nodata']) {
                    html = '<tr><td colspan="7"  class="loading-text">' + data['nodata'] + '</td></tr>';
                    $('#data-table tbody').append(html);
                    $('body').data('status', 'finished');
                    if (cb) {
                        cb();
                    }
                    return false;
                }
                var alive = count(data['data']);
                var result = [];
                for (var oo in data['data']) {
                    for (var key in alive) {
                        alive[key]['time'] = alive[key]['time'] || [];
                        if (data['data'][oo]['hash'] == key) {
                            alive[key]['time'].push((+new Date(data['data'][oo]['date'])));
                            result[key] = data['data'][oo];
                            result[key]['_max'] = Math.max.apply(null, alive[key]['time']);
                            result[key]['_min'] = Math.min.apply(null, alive[key]['time']);
                            result[key]['last_report'] = moment(result[key]['_max']).startOf('hour').fromNow();
                            result[key]['lifespan'] = moment(result[key]['_min']).startOf('hour').fromNow();
                            result[key]['times'] = alive[key]['count'];
                        }
                    }
                }
                for (var oo in result) {
                    var level;
                    if (result[oo]['times'] >= 3) {
                        level = 'danger';
                    } else {
                        level = 'warning';
                    }
                    html += '<tr>' +
                        '<td class="status"><span class="status ' + level + '">status: ' + level + '</span></td>' +
                        '<td class="message ' + level + '">' + result[oo]['message'] + '</td>' +
                        '<td class="file"><a href="view-source:' + result[oo]['file'] + '">' + result[oo]['file'] + ':' + result[oo]['line'] + '</a></td>';
                    html += '<td class="browser"><span class="browser ' + ('internet explorer' == result[oo]['browser']['type'].toLowerCase() ? 'ie' : result[oo]['browser']['type'].toLowerCase()) + '" title="' + result[oo]['browser']['type'] + ' ' + result[oo]['browser']['version'] + '"></span></td>' +
                        '<td class="times">' + result[oo]['times'] + '</td>' +
                        '<td class="lifespan">' + result[oo]['lifespan'] + '</td>' +
                        '<td class="report">' + result[oo]['last_report'] + '</td>' +
                        '</tr>';
                }

                $('#data-table tbody').append(html);
                $('body').data('status', 'finished');
                if (cb) {
                    cb();
                }
            }
            var initBROWERSTable = function (data) {
                var data = data;
                loader('show');
                $('#data-table tbody').empty();
                var html = '';
                if (!data['data'] && data['nodata']) {
                    html = '<tr><td colspan="5"  class="loading-text">' + data['nodata'] + '</td></tr>';
                    $('#data-table tbody').append(html);
                    $('body').data('status', 'finished');
                    if (cb) {
                        cb();
                    }
                    return false;
                }
                data = data['data'];

                var result = {};
                for (var i = 0, j = data.length; i < j; i++) {
                    var tType = data[i]['browser']['type'];
                    result[tType] = result[tType] || {};
                    if (result[tType][data[i]['browser']['version']]) {
                        result[tType][data[i]['browser']['version']] += 1;
                        result[tType]['time'].push((+new Date(data[i]['date'])));
                    } else {
                        result[tType][data[i]['browser']['version']] = 1;
                        result[tType]['time'] = [(+new Date(data[i]['date']))];
                    }
                }

                for (var i in result) {
                    result[i]['_max'] = Math.max.apply(null, result[i]['time']);
                    result[i]['_min'] = Math.min.apply(null, result[i]['time']);
                    result[i]['last_report'] = moment(result[i]['_max']).startOf('hour').fromNow();
                    result[i]['lifespan'] = moment(result[i]['_min']).startOf('hour').fromNow();
                    delete result[i]['time'] && delete result[i]['_max'] && delete result[i]['_min'];
                }

                for (var i in result) {
                    var sum = 0, ver = 0, last = result[i]['last_report'], lifespan = result[i]['lifespan'];
                    delete result[i]['last_report'] && delete result[i]['lifespan'];
                    for (var k in result[i]) {
                        sum += result[i][k];
                        ver += 1;
                    }
                    html += '<tr data-type="' + i + '">';
                    html += '<td class="b-browser"><span class="browser ' + ('internet explorer' == i.toLowerCase() ? 'ie' : i.toLowerCase()) + '" title="' + i + ' ">' + i + '<i class="icon-list-alt" data-cmd="SHOW-BROWSER" title="click to view details."></i></span></td>';
                    html += '<td class="b-times">' + sum + ' (' + ver + ' vers)</td>';
                    html += '<td class="b-last_report">' + last + '</td>';
                    html += '<td class="b-lifespan">' + lifespan + '</td>';
                    html += '</tr>';
                }

                $.jStorage.set('browser', {data: result}, {TTL: cacheTime});

                $('#data-table tbody').append(html);
                $('body').data('status', 'finished');
                if (cb) {
                    cb();
                }

                loader('hide');
            }

            var errData = $.jStorage.get('errors');
            if (errData) {
                switch (mode) {
                    case 'MSG':
                        initMSGTable(errData);
                        break;
                    case 'BROWERS':
                        initBROWERSTable(errData);
                        break;
                }
                return false;
            }


            $.ajax({url: url, dataType: 'json', type: 'GET', success: function (data) {
                $.jStorage.set('errors', data, {TTL: cacheTime});
                switch (mode) {
                    case 'MSG':
                        initMSGTable(data);
                        break;
                    case 'BROWERS':
                        initBROWERSTable(data);
                        break;
                }
            },
                beforeSend: function () {
                    if ($('body').data('status') == 'loading') {
                        console.log('PLZ WAIT FOR THE CURRENT QUERY FINISH.');
                        $('.loading-text').text('等待请求结束 ...');
                        loader('suspend');
                        return false;
                    } else {
                        $('body').data('status', 'loading');
                        $('.loading-text').text('数据正在加载中 ...');
                        loader('show');
                    }
                }
            });
        }

        //init
        $('#data-table').empty().html($('#table-msg-tpl').html());
        initData({'callback': function () {
            $('td.file a').on('click', function (e) {
                window.open($(e.target).attr('href'))
            });
        }, 'mode': 'MSG'});

        var initBtns = function () {
            var initNavBtn = function (target) {
                $('#control-nav').find('li.active').removeClass('active');
                target.closest('li').addClass('active');
            }
            var body = $('body');
            body.on('click', function (e) {
                var target = $(e.target);
                if (target.closest('a[href*=#CMD]')) {
                    e.preventDefault();
                    var cmd = target.attr('href');
                    if (cmd) {
                        cmd = cmd.split('#CMD:')[1];
                        switch (cmd) {
                            case 'MSG':
                                initNavBtn(target);
                                $('#data-table').empty().html($('#table-msg-tpl').html());
                                initData({'callback': function () {
                                    $('td.file a').on('click', function (e) {
                                        window.open($(e.target).attr('href'))
                                    });
                                }, 'mode': 'MSG'});
                                break;
                            case 'SCRIPT':
                            case 'Page':
                                alert('稍后完成。');
                                break;
                            case 'BROWERS':
                                initNavBtn(target);
                                $('#data-table').empty().html($('#table-browser-tpl').html());
                                initData({'callback': function () {
                                    $('i[data-cmd=SHOW-BROWSER]').on('click', function (e) {
                                        var target = $(e.target);
                                        var data = target.closest('tr').data();
                                        data = $.jStorage.get('browser')['data'][data.type];
                                        $('.common-modal').modal('hide').remove();
                                        var modalID = RandomChars(5, 'LCASE');
                                        var tpl = '<div id="' + modalID + '" class="common-modal modal hide fade"></div>';
                                        $('body').append(tpl);
                                        $('#' + modalID).html($('#common-modal-tpl').html()).find('.modal-header h3').text(data.type + ' version list');
                                        var tpl = '<table class="table table-bordered">' +
                                            '<thead><tr><th>version</th><th>submit</th></tr></thead><tbody>';
                                        for (var i in data) {
                                            tpl += '<tr><td>' + i + '</td><td>' + data[i] + '</td></tr>';
                                        }
                                        tpl += '</tbody></table>'
                                        $('#' + modalID).find('.modal-body').html(tpl);
                                        $('#' + modalID).modal('show');
                                    });
                                }, 'mode': 'BROWERS'});
                                break;
                        }
                        console.log(cmd)
                    }
                }
            })
        }
        initBtns();


    });
})(jQuery, "http://soulteary.com")

