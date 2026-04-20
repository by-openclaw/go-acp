import { NetworkClient } from '.';
import { NetworkClientOptions } from '.';
import { LoggingService } from './common/logging/logging.service';

(async (): Promise<void> => {
    const serverOptions = <NetworkClientOptions>{};
    const loggingService = new LoggingService();
    const server = new NetworkClient(loggingService, serverOptions);
    await server.startAsync();
})();
