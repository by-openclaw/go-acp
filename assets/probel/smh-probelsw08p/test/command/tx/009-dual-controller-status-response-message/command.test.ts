import { DualControllerStatusResponseMessageCommand } from '../../../../src/command/tx/009-dual-controller-status-response-message/command';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { DualControllerStatusResponseMessageCommandOptions, ActiveCardBit_0, ActiveCardBit_1, IdleCardStatus } from '../../../../src/command/tx/009-dual-controller-status-response-message/options';

describe('Dual Controller Status Response Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const options: DualControllerStatusResponseMessageCommandOptions = {
                activeCardStatus: ActiveCardBit_0.MASTER_IS_ACTIVE,
                activeStatus: ActiveCardBit_1.ACTIVE,
                idleCardstatus: IdleCardStatus.IDLE_CONTROLLER_IS_OK
            };

            // Act
            const command = new DualControllerStatusResponseMessageCommand(options);

            // Assert
            expect(command).toBeDefined();
        });

    });

    describe('buildCommand', () => {
        describe('General Dual Controller Status Response Message CMD_009_0X09', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.DUAL_CONTROLLER_STATUS_RESPONSE_MESSAGE;
            });

            it('Should create & pack the general command (dualControlleractiveCardStatus = 0 "Master" - dualControlleractiveStatus = 1 "Active" - dualControlleridleCardstatus = 0 "Idle controller is ok")', () => {
                // Arrange
                const options: DualControllerStatusResponseMessageCommandOptions = {
                    activeCardStatus: ActiveCardBit_0.MASTER_IS_ACTIVE,
                    activeStatus: ActiveCardBit_1.ACTIVE,
                    idleCardstatus: IdleCardStatus.IDLE_CONTROLLER_IS_OK
                };

                // Act
                const command = new DualControllerStatusResponseMessageCommand(options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '09 02 00', // data
                    3, // bytesCount
                    0xf2, // checksum
                    '10 02 09 02 00 03 F2 10 03' // buffer
                );
            });
            it('Should create & pack the general command (dualControlleractiveCardStatus = 0 "Master" - dualControlleractiveStatus = 0 "Inactive" - dualControlleridleCardstatus = 0 "Idle controller is ok")', () => {
                // Arrange
                const options: DualControllerStatusResponseMessageCommandOptions = {
                    activeCardStatus: ActiveCardBit_0.MASTER_IS_ACTIVE,
                    activeStatus: ActiveCardBit_1.INACTIVE,
                    idleCardstatus: IdleCardStatus.IDLE_CONTROLLER_IS_OK
                };

                // Act
                const command = new DualControllerStatusResponseMessageCommand(options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '09 00 00', // data
                    3, // bytesCount
                    0xf4, // checksum
                    '10 02 09 00 00 03 F4 10 03' // buffer
                );
            });
            it('Should create & pack the general command (dualControlleractiveCardStatus = 1 "Slave" - dualControlleractiveStatus = 1 "Active" - dualControlleridleCardstatus = 0 "Idle controller is ok")', () => {
                // Arrange
                const options: DualControllerStatusResponseMessageCommandOptions = {
                    activeCardStatus: ActiveCardBit_0.SLAVE_IS_ACTIVE,
                    activeStatus: ActiveCardBit_1.ACTIVE,
                    idleCardstatus: IdleCardStatus.IDLE_CONTROLLER_IS_OK
                };

                // Act
                const command = new DualControllerStatusResponseMessageCommand(options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '09 03 00', // data
                    3, // bytesCount
                    0xf1, // checksum
                    '10 02 09 03 00 03 F1 10 03' // buffer
                );
            });
            it('Should create & pack the general command (dualControlleractiveCardStatus = 0 "Slave" - dualControlleractiveStatus = 0 "Inactive" - dualControlleridleCardstatus = 0 "Idle controller is ok")', () => {
                // Arrange
                const options: DualControllerStatusResponseMessageCommandOptions = {
                    activeCardStatus: ActiveCardBit_0.SLAVE_IS_ACTIVE,
                    activeStatus: ActiveCardBit_1.INACTIVE,
                    idleCardstatus: IdleCardStatus.IDLE_CONTROLLER_IS_OK
                };

                // Act
                const command = new DualControllerStatusResponseMessageCommand(options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '09 01 00', // data
                    3, // bytesCount
                    0xf3, // checksum
                    '10 02 09 01 00 03 F3 10 03' // buffer
                );
            });
            it('Should create & pack the general command (dualControlleractiveCardStatus = 0 "Master" - dualControlleractiveStatus = 0 "Inactive" - dualControlleridleCardstatus = 1 "Idle controller is missing/faulty")', () => {
                // Arrange
                const options: DualControllerStatusResponseMessageCommandOptions = {
                    activeCardStatus: ActiveCardBit_0.MASTER_IS_ACTIVE,
                    activeStatus: ActiveCardBit_1.INACTIVE,
                    idleCardstatus: IdleCardStatus.IDEL_CONTROLLER_IS_MISSING_FAULTY
                };

                // Act
                const command = new DualControllerStatusResponseMessageCommand(options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '09 00 01', // data
                    3, // bytesCount
                    0xf3, // checksum
                    '10 02 09 00 01 03 F3 10 03' // buffer
                );
            });
            it('Should create & pack the general command (dualControlleractiveCardStatus = 1 "Slave" - dualControlleractiveStatus = 0 "Inactive" - dualControlleridleCardstatus = 1 "Idle controller is missing/faulty")', () => {
                // Arrange
                const options: DualControllerStatusResponseMessageCommandOptions = {
                    activeCardStatus: ActiveCardBit_0.SLAVE_IS_ACTIVE,
                    activeStatus: ActiveCardBit_1.INACTIVE,
                    idleCardstatus: IdleCardStatus.IDEL_CONTROLLER_IS_MISSING_FAULTY
                };

                // Act
                const command = new DualControllerStatusResponseMessageCommand(options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '09 01 01', // data
                    3, // bytesCount
                    0xf2, // checksum
                    '10 02 09 01 01 03 F2 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log general command description', () => {
            // Arrange
            const options: DualControllerStatusResponseMessageCommandOptions = {
                activeCardStatus: ActiveCardBit_0.MASTER_IS_ACTIVE,
                activeStatus: ActiveCardBit_1.ACTIVE,
                idleCardstatus: IdleCardStatus.IDLE_CONTROLLER_IS_OK
            };

            // Act
            const command = new DualControllerStatusResponseMessageCommand(options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
