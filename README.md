Error-Tracer (@soulteary)
============

Error Tracer是一个简单的JS+PHP实现的前端错误追踪应用，可以说是为了FIX线上的脚本错误而尝试做的一个比较幼稚的东西。

这个想了好久，大概有几个月了，落实发现不过2，3天，当然，还有很多可以完善的地方。
DEMO版本的界面山寨了NODEJS的[ErrorBoard](https://github.com/Lapple/ErrorBoard)，
DEMO是在SINA APP ENGINE上部署的一个简单的应用，支持主从读写，调试信息输出。



DEMO地址:
http://errorboard.sinaapp.com/

DEMO后台:
http://errorboard.sinaapp.com/push/?mode=admin


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