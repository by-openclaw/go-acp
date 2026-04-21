import { Service } from 'typedi';

import { ApplicationKeepAliveRequestCommand } from '../command/application-keep-alive/application-keepalive-request';
import { ApplicationKeepAliveResponseCommand } from '../command/application-keep-alive/application-keepalive-response';
import { LoggingService } from '../common/logging/logging.service';

@Service()
export class ClientCommandFactory {
    constructor(private _loggingService: LoggingService) {
        _loggingService.trace(() => `${ClientCommandFactory.name} is created with\n`);
    }

    createApplicationKeepAliveRequest(): ApplicationKeepAliveRequestCommand {
        return new ApplicationKeepAliveRequestCommand().buildCommand() as ApplicationKeepAliveRequestCommand;
    }

    createApplicationKeepAliveResponse(): ApplicationKeepAliveResponseCommand {
        return new ApplicationKeepAliveResponseCommand().buildCommand() as ApplicationKeepAliveResponseCommand;
    }
}
