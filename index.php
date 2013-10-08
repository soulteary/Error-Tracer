<!doctype html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <title>TEST</title>
    <script type="text/javascript">
        window.onerror = function ( errorMsg, url, lineNumber ) {
            var e = encodeURIComponent;
            var o = new Image();
            o.src = 'http://errorboard.sinaapp.com/push/?message=' + e( errorMsg ) +
                '&url='     + e( url ) +
                '&line='    + e( lineNumber );
        }
    </script>
    <script type="text/javascript" src="filea.js"></script>
    <script type="text/javascript" src="fileb.js"></script>
    <script type="text/javascript" src="filec.js"></script>

</head>
<body>
<script type="text/javascript">

    www-www


    CREATE TABLE  `app_errorboard`.`test` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY ,
    `file` TEXT CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL COMMENT  '报错脚本路径',
    `line` MEDIUMINT UNSIGNED NOT NULL COMMENT  '报错脚本行号',
    `message` TEXT CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL COMMENT  '报错信息',
    `browsers` SMALLINT UNSIGNED NOT NULL COMMENT  '浏览器类型号',
    `status` TINYINT UNSIGNED NOT NULL DEFAULT  '0' COMMENT  '错误状态',
    `date` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT  '提交时间'
    ) ENGINE = MYISAM CHARACTER SET utf8 COLLATE utf8_general_ci;


    CREATE TABLE  `app_errorboard`.`browser` (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY ,
    `type` VARCHAR( 20 ) NOT NULL ,
    `version` VARCHAR( 20 ) NOT NULL ,
    `date` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    ) ENGINE = MYISAM ;


    messgae
    - status
    true/boolean
    - message
    "console.lok is not a function"
    - source file
    "http://abc.com/error.js:335"
    - browsers
    1,2,3,4,5,6
        - times
    11~20000
    - report time
    11:22:33


    browsers
    - browsers
    id:1
    type:chrome
    version:26

    script
    pages











</script>

<style type="text/css">
    body {
        background: #1F2831 url(/static/img/bg.jpg) top center no-repeat;
        font: 100.1% Calibri, "Segoi UI", "Myriad Pro", "Lucida Grande", "Lucida Sans Unicode", Lucida, Verdana, Helvetica, sans-serif;
        color: #D3D3D3;
        margin: 0;
        padding: 0;
        text-align: center;
    }
    p {
        margin-top: 20px;
    }
    a {
        color: #AAA9A9;
    }
    a:hover {
        color: #F0F0F0;
    }
</style>
<h1>Error Tracer</h1>

<p>View Details: <a href="http://www.soulteary.com/2013/05/21/error-tracer.html" target="_blank">http://www.soulteary.com/2013/05/21/error-tracer.html</a>
</p>
</body>
</html>