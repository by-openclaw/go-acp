import { EventEmitter } from 'events';

import { LoggingService } from './common/logging/logging.service';

/**
 *
 *
 * @export
 * @class CommandParserService
 * @extends {EventEmitter}
 */
export class CommandParserService extends EventEmitter {
    /**
     * Creates an instance of CommandParserService.
     *
     * @param {LoggingService} _loggingService
     * @memberof CommandParserService
     */
    constructor(private _loggingService: LoggingService) {
        super();
        this._loggingService.trace(() => `${CommandParserService.name} is created`);
    }
}
