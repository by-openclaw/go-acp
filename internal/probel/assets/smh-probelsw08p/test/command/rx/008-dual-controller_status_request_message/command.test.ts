import { DualControllerStatusRequestMessageCommand } from '../../../../src/command/rx/008-dual-controller-status-request-message/command';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';

describe('DualControllerStatusRequestMessageCommand (CMD_008_0X08)', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange

            // Act
            const command = new DualControllerStatusRequestMessageCommand();

            // Assert
            expect(command).toBeDefined();
        });
    });

    describe('buildCommand', () => {
        describe('General Dual Controller Status Request Messagee CMD_008-0x08', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.DUAL_CONTROLLER_STATUS_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command', () => {
                // Arrange

                // Act
                const command = new DualControllerStatusRequestMessageCommand();
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '08', // data
                    1, // bytesCount
                    0xf7, // checksum
                    '10 02 08 01 F7 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange

            // Act
            const command = new DualControllerStatusRequestMessageCommand();
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
