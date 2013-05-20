;
(function () {
    var timer = 0;
    var hasLoaded = false;
    var PageLoading = function () {
        var index = 0;
        var scroll = document.getElementById('finish');
        var init = function () {
            if (index > 95) {
                index = 0;
            }
            index++;
            scroll.style.left = index + 'px';
            if (!hasLoaded) {
                timer = setTimeout(arguments.callee, 10);
            }
        }
        init();
    }
    PageLoading();
})();

;
(function ($) {
    $(document).ready(function () {

        var initData = function (params) {
            var url =params.url || '?mode=admin&a=query';
            $.ajax({url: url, dataType: 'json', type: 'POST', success: function (data) {
                $('#loader').hide();
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
                    html += '<tr><td><span class="status '+level+'">status: '+level+'</span></td><td class="'+level+'">'+result[oo]['message']+'</td><td><a href="view-source:'+result[oo]['file']+'">'+result[oo]['file']+':'+result[oo]['line']+'</a></td><td><span class="browser '+result[oo]['browser']['type'].toLowerCase()+'" title="'+result[oo]['browser']['type']+' '+result[oo]['browser']['version']+'"></span></td><td>'+result[oo]['times']+'</td><td>'+result[oo]['lifespan']+'</td><td>'+result[oo]['last_report']+'</td></tr>';
                }

                $('#data-table tbody').append(html);

            },
                beforeSend: function () {
                    if($('body').attr('data-status')=='loading'){
                        console.log('PLZ WAIT FOR THE CURRENT QUERY FINISH.');
                        $('#loader p').text('等待请求结束 ...');
                        return;
                    }else{
                        $('body').attr('data-status','loading');
                        $('#loader p').text('页面正在加载中 ...');
                        $('#loader').show();
                    }
                }
            });
        }
        initData({})
        var initBtns = function () {
            var body = $('body');
            body.on('click', function (e) {
                var target = $(e.target);
                if (target.closest('a[href^=#CMD]')) {
                    e.preventDefault();
                    var cmd = target.attr('href');
                    if (cmd) {
                        cmd = cmd.split('#CMD:')[1];

                        switch (cmd) {
                            case 'SCRIPT':
                            case 'Page':
                            case 'BROWERS':
                                alert('稍后完成。');
                                break;
                        }
                        console.log(cmd);
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

