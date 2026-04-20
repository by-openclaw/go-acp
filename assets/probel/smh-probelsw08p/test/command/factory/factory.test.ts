import createMockInstance from 'jest-create-mock-instance';
import { LoggingService } from '../../../src/common/logging/logging.service';
import { CommandFactory } from '../../../src/command/factory/command-factory';
import { CrossPointInterrogateMessageCommand } from '../../../src/command/rx/001-crosspoint-interrogate-message/command';
import { CrossPointInterrogateMessageCommandParams } from '../../../src/command/rx/001-crosspoint-interrogate-message/params';

describe('CommandFactory', () => {
    let loggingService: jest.Mocked<LoggingService>;

    beforeAll(async () => {
        loggingService = createMockInstance(LoggingService);
    });

    it('should create a command from params', () => {

        // Arrange
        const params: CrossPointInterrogateMessageCommandParams = {
            matrixId: 0,
            levelId: 0,
            destinationId: 0
        };
        // Act
        const command = CommandFactory.fromParams(params, {}, CrossPointInterrogateMessageCommand);

        // Assert
        console.log(command.toHexDump());
    });
    it('should create a command from buffer', () => {

        // Arrange
        const buffer = Buffer.from([0x10, 0x02, 0x01, 0x00, 0x00, 0x00, 0x04, 0xfb, 0x10, 0x03]); 

        // Act
        const command = CommandFactory.fromBuffer(buffer);

        // Assert
        console.log(command.toHexDump());
    });
});
