import { LoggingService } from '../common/logging/logging.service';
/**
 *
 *
 * @export
 * @class NetworkSocket
 */
export class NetworkSocket {
    /**
     * Creates an instance of NetworkSocket.
     * @param {LoggingService} _loggingService
     * @memberof NetworkSocket
     */
    constructor(private _loggingService: LoggingService) {
        this._loggingService.trace(() => `${NetworkSocket.name}`);
    }
}
