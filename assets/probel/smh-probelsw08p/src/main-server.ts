import { NetworkServer } from '.';
import { NetworkServerOptions } from '.';
import { CommandParserService } from './command-parser';
import { LoggingService } from './common/logging/logging.service';
import { AsyncUtility } from './common/utility/async.utility';
import { JsonUtility } from './common/utility/json.utility';

(async (): Promise<void> => {
    const serverOptions = <NetworkServerOptions>{
        maxConnections: 2
    };
    const loggingService = new LoggingService();
    const dataLayerDecoderService = new CommandParserService(loggingService);
    const server = new NetworkServer(loggingService, dataLayerDecoderService, serverOptions);
    const address = '127.0.0.1';
    const port = 9000;
    await server.startAsync(address, port);
    await AsyncUtility.delayAsync(150000);

    loggingService.trace(() => `server.boundAddress:${JsonUtility.stringify(server.boundAddress)}`);
    loggingService.trace(() => `server.isListening:${server.isListening}`);
    loggingService.trace(() => `server.status:${server.status}`);
    loggingService.trace(() => `server.getMaxListeners:${server.getMaxListeners()}`);

    await server.stopAsync();
})();
