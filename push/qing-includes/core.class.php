<?php
/**
 * Error Tracer
 * 程序核心函数库。
 *
 * @version 1.0.0
 * @base Qing By soulteary
 * @include
 *          - @function associative_push            创建关联数组
 *          - @function init_args                   初始化传递参数
 *          - @function get_mircotime               输出毫秒时间
 *          - @function gzip_accepted               判断服务器是否支持GZIP
 *          - @function message                     显示系统消息
 *          - @function json                        以JSON格式显示系统消息
 *
 * @package soulteary
 * @email   soulteary@qq.com
 * @website http://soulteary.com
 */

if(!defined('FILE_PREFIX'))die('Silence is golden.');

class Core
{
    /**
     * 创建关联数组
     *
     * @since 1.0.0
     *
     * @eg. $result = core::associative_push($target, $data);
     * @param array $arr 要被创建的关联数组。
     * @param array $tmp 要被填充为新数组内容的临时数组。
     * @return array $arr 创建好的关联数组。
     */
    public function associative_push($arr, $tmp)
    {
        if (is_array($tmp)) {
            foreach ($tmp as $key => $value) {
                $arr[$key] = $value;
            }
            return $arr;
        }
        return false;
    }

    /**
     * 初始化传递参数
     *
     * @since 1.0.0
     *
     * @use core::associative_push();
     * @eg. $this->args = core::init_args(func_get_args());
     * @param array $args 传递进来的参数。
     * @return array $result 序列化好的新数组。
     */
    public function init_args($args)
    {
        $result = array();
        for ($i = 0, $n = count($args); $i < $n; $i++) {
            $result = self::associative_push($args[$i], $result);
        }
        return $result;
    }

    /**
     * 输出毫秒时间。
     *
     * @since 1.0.0
     *
     * @eg. core::get_mircotime();
     * @return float 当前时间的毫秒时间。
     */
    public function get_mircotime(){
        list($usec, $sec) = explode(" ", microtime());
        return ((float)$usec + (float)$sec);
    }

    /**
     * 判断服务器是否支持GZIP，并防止重复压缩。
     *
     * @since 1.0.0
     *
     * @eg. core::gzip_accepted();
     * @return boolean 服务器是否支持GZIP。
     */
    public function gzip_accepted()
    {
        $enable = strtolower(ini_get('zlib.output_compression'));
        if (1 == $enable || "on" == $enable ){
            return false;
        }

        if ( !isset($_SERVER['HTTP_ACCEPT_ENCODING']) || (isset($_SERVER['HTTP_ACCEPT_ENCODING']) && strpos($_SERVER['HTTP_ACCEPT_ENCODING'], 'gzip' ) === false ))
        {
            return false;
        }

        return true;
    }

    /**
     * 显示系统消息
     *
     * @since 0.0.1
     *
     * @param string $msg HEADER消息头部。
     * @param string $url 要转向的地址。
     * @param boolean $isAutoGo 是否自动转向。
     * @return mixed HTML消息页面。
     */
    public function message($msg, $url = 'javascript:history.back(-1);', $isAutoGo = false)
    {
        if(is_array($msg)){
            if(is_array($msg['message'])){
                array_walk($msg['message'], function(&$n) {
                    $n = "<p>$n</p>\n";
                });
                $msg['message'] = implode('',$msg['message']);
            }
            $msg = '<h2>'.$msg['title'].'</h2>'.$msg['message'];
        }
        else{
            if ($msg == '404') {
                header("HTTP/1.1 404 Not Found");
                $msg = '<p>404 请求页面不存在！</p>';
            }
        }

        echo <<<EOT
<!doctype html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
EOT;
        if ($isAutoGo) {
            echo "<meta http-equiv=\"refresh\" content=\"2;url=$url\" />";
        }
        echo <<<EOT
    <title>系统消息</title>
    <style type="text/css">
        body {
            background-color: #F7F7F7;
            font-family: Arial;
            font-size: 12px;
            line-height: 150%;
        }
        .main {
            position: absolute;
            width: 580px;
            min-height: 70px;
            top: 20%;
            left: 50%;
            margin-left: -290px;
            margin-top: -35px;
            background-color: #FFF;
            border: 1px solid #DFDFDF;
            box-shadow: 1px 1px #E4E4E4;
            padding: 10px;
        }
        .main p {
            color: #666;
            line-height: 1.5;
            font-size: 12px;
            margin: 13px 20px;
        }
        .main a {
            margin: 0 5px;
            color: #11A1DA;
        }
        .main a:hover {
            color: #34B7EB;
        }
    </style>
</head>
<body>
    <div class="main">
        $msg
        <p><a href="$url">&laquo;点击返回</a></p>
    </div>
</body>
</html>
EOT;
        exit;
    }

    /**
     * 以JSON格式显示系统消息
     *
     * @since 0.0.1
     *
     * @param string $msg HEADER消息头部。
     * @param string $url 要转向的地址。
     * @param boolean $isAutoGo 是否自动转向。
     * @return JSON
     */
    public function json($data)
    {
        header('Extra Data: Javascript Error Tracer v1.0.0');
        header('Access-Control-Allow-Origin: *');
        header('Content-type:text/html; charset=UTF-8');
        header('Cache-Control: no-cache');
        header('Pragma: no-cache');
        header("Content-type:application/json");
        echo json_encode($data);
        exit();
    }

}
?>