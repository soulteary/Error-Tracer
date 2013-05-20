Error-Tracer (@soulteary)
============
![admin page](http://ww4.sinaimg.cn/mw690/48ba00e9jw1e4v8deoqtcj20sg0c7tc2.jpg)
Error Tracer 是一个简单的JS+PHP实现的前端错误追踪应用，可以说是为了FIX线上的脚本错误而尝试做的一个比较幼稚的东西。
这个想法几个月之前就有，实际落实发现不过2，3天时间，当然，程序尚有很多可以完善的地方。
DEMO版本的界面山寨了NODEJS的[ErrorBoard](https://github.com/Lapple/ErrorBoard)， 现在跑在SINA APP ENGINE。

目前的特性：
1.数据库主从分离。
2.自动添加浏览器信息。
3.自动合并和计算同类型的错误信息。
4.自动计算BUG生命周期。

打算加入的features:
1.浏览器信息支持列表+自动添加。
2.合并和计算同类型信息更精确。
3.完成修复的BUG直接在线删除或者标记。
4.支持根据浏览器，IP，脚本，行数，来查找和追踪触发BUG的人群。
5.汇总邮件。

DEMO地址: http://errorboard.sinaapp.com/
DEMO后台: http://errorboard.sinaapp.com/push/?mode=admin


数据库结构:

	CREATE TABLE `browser` (
	 `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
	 `type` varchar(20) NOT NULL,
	 `version` varchar(20) NOT NULL,
	 `date` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
	 PRIMARY KEY (`id`)
	) ENGINE=MyISAM AUTO_INCREMENT=27 DEFAULT CHARSET=utf8

	CREATE TABLE `error` (
	 `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
	 `date` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '错误时间',
	 `ip` decimal(11,0) NOT NULL,
	 PRIMARY KEY (`id`)
	) ENGINE=MyISAM AUTO_INCREMENT=171 DEFAULT CHARSET=utf8
	
	CREATE TABLE `test` (
	 `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
	 `file` text NOT NULL COMMENT '报错脚本路径',
	 `line` mediumint(8) unsigned NOT NULL COMMENT '报错脚本行号',
	 `message` text NOT NULL COMMENT '报错信息',
	 `browser` smallint(5) unsigned NOT NULL COMMENT '浏览器类型号',
	 `status` tinyint(3) unsigned NOT NULL DEFAULT '0' COMMENT '错误状态',
	 `date` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '提交时间',
	 `ip` decimal(11,0) NOT NULL,
	 `platform` varchar(15) NOT NULL DEFAULT 'unknown' COMMENT '操作系统',
	 PRIMARY KEY (`id`)
	) ENGINE=MyISAM AUTO_INCREMENT=271 DEFAULT CHARSET=utf8