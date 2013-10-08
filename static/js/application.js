;
(function ($) {
    $(document).ready(function () {
        var loader = function(mode){
            $('.progress .bar').attr('class','bar');
            if(mode == 'show'){
                $('.progress').fadeIn().find('.bar').css('width','100%');
            }else if(mode == 'hide'){
                $('.progress').find('.bar').addClass('bar-success').closest('.progress').fadeOut().find('.bar').css('width',0);
            }else if(mode == 'suspend'){
                $('.progress').find('.bar').addClass('bar-warning');
            }
        }

        var initData = function (params) {
            var url =params.url || '?mode=admin&a=query';
            var cb = params.callback || null;
            $.ajax({url: url, dataType: 'json', type: 'POST', success: function (data) {
                loader('hide');
                $('#data-table tbody').empty();
                var html = '';
                var alive = count(data['data']);
                var result = [];
                for(var oo in data['data']){
                    for(var key in alive){
                        alive[key]['time'] = alive[key]['time'] || [];
                        if(data['data'][oo]['hash'] == key){
                            alive[key]['time'].push((+new Date(data['data'][oo]['date'])));
                            result[key] = data['data'][oo];
                            result[key]['_max'] =  Math.max.apply(null, alive[key]['time']);
                            result[key]['_min'] =  Math.min.apply(null, alive[key]['time']);
                            result[key]['last_report'] =  moment(result[key]['_max']).startOf('hour').fromNow();
                            result[key]['lifespan'] =  moment(result[key]['_min']).startOf('hour').fromNow();
                            result[key]['times'] = alive[key]['count'];
                        }
                    }
                }

                for(var oo in result){
                    var level;
                    if(result[oo]['times']>=3){
                        level = 'danger';
                    }else{
                        level = 'warning';
                    }
                    if('internet explorer' == result[oo]['browser']['type'].toLowerCase()){
                        result[oo]['browser']['type'] = 'ie';
                    }
                    html += '<tr>'+
                        '<td class="status"><span class="status '+level+'">status: '+level+'</span></td>' +
                        '<td class="message '+level+'">'+result[oo]['message']+'</td>' +
                        '<td class="file"><a href="view-source:'+result[oo]['file']+'">'+result[oo]['file']+':'+result[oo]['line']+'</a></td>' +
                        '<td class="browser"><span class="browser '+result[oo]['browser']['type'].toLowerCase()+'" title="'+result[oo]['browser']['type']+' '+result[oo]['browser']['version']+'"></span></td>' +
                        '<td class="times">'+result[oo]['times']+'</td>' +
                        '<td class="lifespan">'+result[oo]['lifespan']+'</td>' +
                        '<td class="report">'+result[oo]['last_report']+'</td>' +
                        '</tr>';
                }

                $('#data-table tbody').append(html);
                $('body').data('status','finished');
                if(cb){cb();}
            },
                beforeSend: function () {
                    if($('body').data('status')=='loading'){
                        console.log('PLZ WAIT FOR THE CURRENT QUERY FINISH.');
                        $('.loading-text').text('等待请求结束 ...');
                        loader('suspend');
                        return;
                    }else{
                        $('body').data('status','loading');
                        $('.loading-text').text('页面正在加载中 ...');
                        loader('show');
                    }
                }
            });
        }

        //init
        $('#data-table').empty().html($('#table-msg-tpl').html());
        initData({'callback':function(){
            $('td.file a').on('click',function(e){window.open($(e.target).attr('href'))});
        }});

        var initBtns = function () {
            var initNavBtn = function(target){
                $('#control-nav').find('li.active').removeClass('active');
                target.closest('li').addClass('active');
            }
            var body = $('body');
            body.on('click', function (e) {
                var target = $(e.target);
                if (target.closest('a[href*=#CMD]')) {
                    e.preventDefault();
                    var cmd = target.attr('href');
                        cmd = cmd.split('#CMD:')[1];
                    if (cmd) {
                        switch (cmd) {
                            case 'MSG':
                                initNavBtn(target);
                                $('#data-table').empty().html($('#table-msg-tpl').html());
                                initData({'callback':function(){
                                    $('td.file a').on('click',function(e){window.open($(e.target).attr('href'))});
                                }});
                                break;
                            case 'SCRIPT':
                            case 'Page':
                                alert('稍后完成。');
                                break;
                            case 'BROWERS':
                                initNavBtn(target);
                                $('#data-table thead').remove()
                                break;
                        }
                        console.log(cmd)
                    }
                }
            })
        }
        initBtns();

        var count = function(data){
            var mapReduce = {
                map : function(object) {
                    return {key : object['hash'], value : 1};
                },
                reduce : function(allSteps) {
                    var result = {};
                    for(var i=0; i<allSteps.length; i++)
                    {
                        var step = allSteps[i];
                        result[step.key] = result[step.key] ? (result[step.key] + 1) : 1;
                    }
                    return result;
                },
                init : function() {
                    var allSteps = [];
                    for(var i=0; i<data.length; i++){
                        allSteps = allSteps.concat(mapReduce.map(data[i]));
                    }
                    var tmp = mapReduce.reduce(allSteps);
                    var result = [];
                    for(var oo in tmp){
                        result[oo] = [];
                        result[oo]['count'] = tmp[oo];
                    }
                    return result;
                }
            }
            return mapReduce.init();
        }

    });
})(jQuery, "http://soulteary.com")

