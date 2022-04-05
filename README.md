# runsm

This starts and runs the ScoreMaster server services.

>**runsm**  
**-ip** *ip interfaces* (default '*')  
**-port** *port* (default '80')  
**-altport** *port* (default '2015')  
**-nolocal**  (Don't launch browser interface on server)  
**-respawn** *minutes* (Respawn php-cgi interval. Default '60')  
**-debug**  (Run PHP as local server for debugging purposes)

The **-altport** is used if **-port** is unavailable because, for example, insufficient privilege.

This calls three services:- PHP, Caddy & EBCFetch.

PHP is used in CGI mode and consequently needs to be periodically respawned so that bad things don't happen.

Caddy is a standard webserver

EBCFetch is the ScoreMaster Electronic Bonus Claim fetcher maintained separately.

