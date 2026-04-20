import { LoggingService } from '../common/logging/logging.service';
import { JsonUtility } from '../common/utility/json.utility';
import { NetworkClientOptions } from './network-client.options';

export class NetworkClient {
    constructor(private _loggingService: LoggingService, private _options: NetworkClientOptions) {
        _loggingService.trace(() => `${NetworkClient.name} is created with\n`, JsonUtility.stringify(_options));
    }

    startAsync(): Promise<void> {
        this._loggingService.trace(() => `${NetworkClient.name}|${this.startAsync.name}...`);
        return Promise.resolve();
    }

    stopAsync(): Promise<void> {
        this._loggingService.trace(() => `${NetworkClient.name}|${this.stopAsync.name}...`);
        return Promise.resolve();
    }
}
